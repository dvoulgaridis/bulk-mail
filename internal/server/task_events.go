package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/store"
	taskpkg "github.com/dvoulgaridis/bulk-mail/internal/tasks"
)

const (
	taskEventCoalesce  = 500 * time.Millisecond
	taskEventKeepalive = 15 * time.Second
)

type taskStreamEvent struct {
	Name  string
	Tasks []taskpkg.Task
}

type taskStreamPayload struct {
	Tasks []taskpkg.Task `json:"tasks"`
}

type taskEventHub struct {
	repo *store.Store

	ctx       context.Context
	cancel    context.CancelFunc
	wake      chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	mu          sync.Mutex
	closed      bool
	dirty       map[int64]struct{}
	subscribers map[chan taskStreamEvent]struct{}
}

func newTaskEventHub(repo *store.Store) *taskEventHub {
	ctx, cancel := context.WithCancel(context.Background())
	hub := &taskEventHub{
		repo: repo, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		dirty: map[int64]struct{}{}, subscribers: map[chan taskStreamEvent]struct{}{},
	}
	go hub.run()
	return hub
}

func (hub *taskEventHub) MarkChanged(taskID int64) {
	if taskID <= 0 {
		return
	}
	hub.mu.Lock()
	if hub.closed {
		hub.mu.Unlock()
		return
	}
	hub.dirty[taskID] = struct{}{}
	hub.mu.Unlock()
	select {
	case hub.wake <- struct{}{}:
	default:
	}
}

func (hub *taskEventHub) Subscribe() (<-chan taskStreamEvent, bool) {
	updates := make(chan taskStreamEvent, 4)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		close(updates)
		return updates, false
	}
	hub.subscribers[updates] = struct{}{}
	return updates, true
}

func (hub *taskEventHub) Unsubscribe(updates <-chan taskStreamEvent) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for subscriber := range hub.subscribers {
		if subscriber == updates {
			delete(hub.subscribers, subscriber)
			close(subscriber)
			return
		}
	}
}

func (hub *taskEventHub) Close() {
	hub.closeOnce.Do(func() {
		hub.mu.Lock()
		hub.closed = true
		hub.mu.Unlock()
		hub.cancel()
	})
	<-hub.done
}

func (hub *taskEventHub) run() {
	defer close(hub.done)
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-hub.wake:
			if timer == nil {
				timer = time.NewTimer(taskEventCoalesce)
				timerC = timer.C
			}
		case <-timerC:
			hub.publishDirty()
			timer = nil
			timerC = nil
		case <-hub.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			hub.closeSubscribers()
			return
		}
	}
}

func (hub *taskEventHub) publishDirty() {
	hub.mu.Lock()
	ids := make([]int64, 0, len(hub.dirty))
	for id := range hub.dirty {
		ids = append(ids, id)
	}
	hub.dirty = map[int64]struct{}{}
	hub.mu.Unlock()

	tasks := make([]taskpkg.Task, 0, len(ids))
	for _, id := range ids {
		task, err := hub.repo.GetTask(hub.ctx, id)
		if err == nil {
			tasks = append(tasks, task)
		}
	}
	if len(tasks) == 0 {
		return
	}
	hub.broadcast(taskStreamEvent{Name: "tasks-updated", Tasks: tasks})
}

func (hub *taskEventHub) broadcast(event taskStreamEvent) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for subscriber := range hub.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(hub.subscribers, subscriber)
			close(subscriber)
		}
	}
}

func (hub *taskEventHub) closeSubscribers() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for subscriber := range hub.subscribers {
		delete(hub.subscribers, subscriber)
		close(subscriber)
	}
}

func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "event streaming is unavailable")
		return
	}
	updates, ok := s.taskEvents.Subscribe()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "event streaming is shutting down")
		return
	}
	defer s.taskEvents.Unsubscribe(updates)

	tasks, err := s.repo.ListTasks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []taskpkg.Task{}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := io.WriteString(w, "retry: 1000\n\n"); err != nil {
		return
	}
	if err := writeTaskStreamEvent(w, taskStreamEvent{Name: "tasks-snapshot", Tasks: tasks}); err != nil {
		return
	}
	flusher.Flush()

	keepalive := time.NewTicker(taskEventKeepalive)
	defer keepalive.Stop()
	for {
		select {
		case event, open := <-updates:
			if !open || writeTaskStreamEvent(w, event) != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeTaskStreamEvent(w io.Writer, event taskStreamEvent) error {
	payload, err := json.Marshal(taskStreamPayload{Tasks: event.Tasks})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "event: "+event.Name+"\ndata: "+string(payload)+"\n\n")
	return err
}
