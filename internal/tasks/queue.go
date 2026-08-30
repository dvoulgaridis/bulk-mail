package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

var ErrQueueFull = errors.New("task queue is full")

// Queue contracts and data.

type QueueRepository interface {
	EnqueueTask(
		context.Context,
		int64,
		int,
		Metadata,
		string,
		int64,
		int,
	) (Task, error)
	ClaimNextTask(context.Context) (int64, bool, error)
	GetTaskInput(context.Context, int64) (Input, error)
	GetQueuedTaskInput(context.Context, int64) (Input, error)
	TakeTaskInput(context.Context, int64) (Input, error)
	ListInterruptedTaskIDs(context.Context) ([]int64, error)
	ReconcileTaskInputs(context.Context) ([]string, []string, error)
}

type Submission struct {
	CampaignID int64
	ProfileID  int64
	Total      int
	Metadata   Metadata
	Manifest   []byte
	Files      []StoredFile
}

type Payload struct {
	manifest []byte
	storage  *Storage
	key      string
}

type Queue struct {
	repo      QueueRepository
	storage   *Storage
	runner    *Runner
	maxQueued int
	notify    func(int64)
}

type RecoverInterruptedFunc func(context.Context, int64, []byte, error) error

type FinalizeQueuedCancellationFunc func(context.Context, int64, []byte) (bool, error)

// Payload access.

func (payload Payload) Manifest() []byte {
	return payload.manifest
}

func (payload Payload) ReadFile(name string) ([]byte, error) {
	return payload.storage.ReadFile(payload.key, name)
}

// Construction and lifecycle.

func NewQueue(
	parent context.Context,
	repo QueueRepository,
	storageRoot string,
	maxConcurrent int,
	maxQueued int,
	notify func(int64),
) (*Queue, error) {
	if repo == nil {
		return nil, errors.New("task queue repository is required")
	}
	if maxConcurrent < 1 {
		return nil, errors.New("maximum concurrent tasks must be at least one")
	}
	if maxQueued < 1 {
		return nil, errors.New("maximum queued tasks must be at least one")
	}
	storage, err := OpenStorage(storageRoot)
	if err != nil {
		return nil, err
	}
	if notify == nil {
		notify = func(int64) {}
	}
	return &Queue{
		repo:      repo,
		storage:   storage,
		runner:    New(parent, maxConcurrent),
		maxQueued: maxQueued,
		notify:    notify,
	}, nil
}

func (queue *Queue) Start(
	execute ExecuteFunc,
	recoverInterrupted RecoverInterruptedFunc,
	cancelQueued FinalizeQueuedCancellationFunc,
) error {
	if execute == nil || recoverInterrupted == nil || cancelQueued == nil {
		return errors.New("task queue callbacks are required")
	}
	if err := queue.recoverInterruptedTasks(context.Background(), recoverInterrupted); err != nil {
		return fmt.Errorf("recover interrupted tasks: %w", err)
	}
	if err := queue.reconcileStorage(context.Background()); err != nil {
		return fmt.Errorf("reconcile task queue: %w", err)
	}
	return queue.runner.Start(
		queue.claimNextTask,
		func(ctx context.Context, taskID int64) {
			defer queue.releaseTaskInput(taskID)
			execute(ctx, taskID)
		},
		func(ctx context.Context, taskID int64) (bool, error) {
			return queue.cancelQueuedTask(ctx, taskID, cancelQueued)
		},
	)
}

func (queue *Queue) Shutdown(ctx context.Context) error {
	return queue.runner.Shutdown(ctx)
}

// Task submission and payload loading.

func (queue *Queue) Submit(ctx context.Context, submission Submission) (Task, error) {
	storageKey, err := queue.storage.Stage(submission.Manifest, submission.Files)
	if err != nil {
		return Task{}, fmt.Errorf("stage task input: %w", err)
	}
	task, err := queue.repo.EnqueueTask(
		ctx,
		submission.CampaignID,
		submission.Total,
		submission.Metadata,
		storageKey,
		submission.ProfileID,
		queue.maxQueued,
	)
	if err != nil {
		if cleanupErr := queue.storage.Remove(storageKey); cleanupErr != nil {
			slog.Warn("remove rejected task storage", "error", cleanupErr)
		}
		return Task{}, err
	}
	queue.notify(task.ID)
	queue.runner.Notify()
	return task, nil
}

func (queue *Queue) LoadPayload(ctx context.Context, taskID int64) (Payload, error) {
	input, err := queue.repo.GetTaskInput(ctx, taskID)
	if err != nil {
		return Payload{}, err
	}
	manifest, err := queue.storage.ReadManifest(input.StorageKey)
	if err != nil {
		return Payload{}, err
	}
	return Payload{manifest: manifest, storage: queue.storage, key: input.StorageKey}, nil
}

// Cancellation.

func (queue *Queue) Cancel(ctx context.Context, taskID int64) (bool, error) {
	return queue.runner.Cancel(ctx, taskID)
}

// Runner callbacks and task-input cleanup.

func (queue *Queue) claimNextTask(ctx context.Context) (int64, bool, error) {
	taskID, found, err := queue.repo.ClaimNextTask(ctx)
	if err != nil || !found {
		return taskID, found, err
	}
	queue.notify(taskID)
	return taskID, true, nil
}

func (queue *Queue) cancelQueuedTask(
	ctx context.Context,
	taskID int64,
	finalize FinalizeQueuedCancellationFunc,
) (bool, error) {
	input, err := queue.repo.GetQueuedTaskInput(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	manifest, err := queue.storage.ReadManifest(input.StorageKey)
	if err != nil {
		return false, err
	}
	cancelled, err := finalize(ctx, taskID, manifest)
	if err != nil || !cancelled {
		return cancelled, err
	}
	if err := queue.storage.Remove(input.StorageKey); err != nil {
		slog.Warn("remove cancelled task storage", "task_id", taskID, "error", err)
	}
	queue.notify(taskID)
	return true, nil
}

func (queue *Queue) releaseTaskInput(taskID int64) {
	input, err := queue.repo.TakeTaskInput(context.Background(), taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Warn("release task input", "task_id", taskID, "error", err)
		return
	}
	if err := queue.storage.Remove(input.StorageKey); err != nil {
		slog.Warn("remove completed task storage", "task_id", taskID, "error", err)
	}
}

// Queue recovery.

func (queue *Queue) recoverInterruptedTasks(
	ctx context.Context,
	recoverInterrupted RecoverInterruptedFunc,
) error {
	taskIDs, err := queue.repo.ListInterruptedTaskIDs(ctx)
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		input, inputErr := queue.repo.GetTaskInput(ctx, taskID)
		var manifest []byte
		if inputErr == nil {
			manifest, inputErr = queue.storage.ReadManifest(input.StorageKey)
		}
		if err := recoverInterrupted(ctx, taskID, manifest, inputErr); err != nil {
			return err
		}
		if inputErr != nil {
			continue
		}
		if _, err := queue.repo.TakeTaskInput(ctx, taskID); err != nil {
			return err
		}
		if err := queue.storage.Remove(input.StorageKey); err != nil {
			return err
		}
	}
	return nil
}

func (queue *Queue) reconcileStorage(ctx context.Context) error {
	retained, stale, err := queue.repo.ReconcileTaskInputs(ctx)
	if err != nil {
		return err
	}
	for _, key := range stale {
		if err := queue.storage.Remove(key); err != nil {
			return err
		}
	}
	keep := make(map[string]struct{}, len(retained))
	for _, key := range retained {
		keep[key] = struct{}{}
	}
	return queue.storage.Prune(keep)
}
