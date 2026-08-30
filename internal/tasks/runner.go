package tasks

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrStopped     = errors.New("task runner is stopped")
	ErrCancelled   = errors.New("task cancelled by operator")
	ErrInterrupted = errors.New("task interrupted by application shutdown")
)

// Runner callbacks and state.

type activeTask struct {
	cancel context.CancelCauseFunc
}

type ClaimFunc func(context.Context) (int64, bool, error)

type ExecuteFunc func(context.Context, int64)

type CancelQueuedFunc func(context.Context, int64) (bool, error)

type Runner struct {
	ctx           context.Context
	stop          context.CancelCauseFunc
	maxConcurrent int
	wake          chan struct{}

	mu           sync.Mutex
	started      bool
	closed       bool
	claim        ClaimFunc
	execute      ExecuteFunc
	cancelQueued CancelQueuedFunc
	active       map[int64]activeTask
	wg           sync.WaitGroup
}

// Construction and lifecycle.

func New(parent context.Context, maxConcurrent int) *Runner {
	if maxConcurrent < 1 {
		panic("task runner requires maximum concurrency of at least one")
	}
	ctx, stop := context.WithCancelCause(parent)
	return &Runner{
		ctx:           ctx,
		stop:          stop,
		maxConcurrent: maxConcurrent,
		wake:          make(chan struct{}, maxConcurrent),
		active:        map[int64]activeTask{},
	}
}

func (runner *Runner) Start(claim ClaimFunc, execute ExecuteFunc, cancelQueued CancelQueuedFunc) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.closed {
		return ErrStopped
	}
	if runner.started {
		return errors.New("task runner is already started")
	}
	if claim == nil || execute == nil || cancelQueued == nil {
		return errors.New("task runner callbacks are required")
	}
	runner.started = true
	runner.claim = claim
	runner.execute = execute
	runner.cancelQueued = cancelQueued
	runner.wg.Add(runner.maxConcurrent)
	for range runner.maxConcurrent {
		go runner.worker()
	}
	runner.notifyLocked()
	return nil
}

func (runner *Runner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	if !runner.closed {
		runner.closed = true
		runner.stop(ErrInterrupted)
		for _, task := range runner.active {
			task.cancel(ErrInterrupted)
		}
	}
	runner.mu.Unlock()

	done := make(chan struct{})
	go func() {
		runner.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Work notification and cancellation.

func (runner *Runner) Notify() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if !runner.closed && runner.started {
		runner.notifyLocked()
	}
}

func (runner *Runner) Cancel(ctx context.Context, id int64) (bool, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.closed || !runner.started {
		return false, nil
	}
	if task, exists := runner.active[id]; exists {
		task.cancel(ErrCancelled)
		return true, nil
	}
	return runner.cancelQueued(ctx, id)
}

// Worker execution and coordination.

func (runner *Runner) worker() {
	defer runner.wg.Done()
	for {
		id, taskContext, found, err := runner.claimTask()
		if errors.Is(err, ErrStopped) {
			return
		}
		if err != nil {
			slog.Error("claim queued task", "error", err)
			if !runner.waitForWork(time.Second) {
				return
			}
			continue
		}
		if !found {
			if !runner.waitForWork(0) {
				return
			}
			continue
		}
		runner.execute(taskContext, id)
		runner.mu.Lock()
		delete(runner.active, id)
		runner.mu.Unlock()
	}
}

func (runner *Runner) claimTask() (int64, context.Context, bool, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.closed {
		return 0, nil, false, ErrStopped
	}
	id, found, err := runner.claim(runner.ctx)
	if err != nil {
		return 0, nil, false, err
	}
	if !found {
		return 0, nil, false, nil
	}
	ctx, cancel := context.WithCancelCause(runner.ctx)
	runner.active[id] = activeTask{cancel: cancel}
	return id, ctx, true, nil
}

func (runner *Runner) waitForWork(retryAfter time.Duration) bool {
	var retry <-chan time.Time
	var timer *time.Timer
	if retryAfter > 0 {
		timer = time.NewTimer(retryAfter)
		retry = timer.C
		defer timer.Stop()
	}
	select {
	case <-runner.wake:
		return true
	case <-retry:
		return true
	case <-runner.ctx.Done():
		return false
	}
}

func (runner *Runner) notifyLocked() {
	for range runner.maxConcurrent {
		select {
		case runner.wake <- struct{}{}:
		default:
			return
		}
	}
}
