package app

import (
	"context"
	"fmt"
	"sync"
)

// Each campaign execution and preflight owns an independent prepared-attachment budget.
const preparedAttachmentBudgetBytes int64 = 200 << 20

type attachmentBudget struct {
	limit   int64
	used    int64
	changed chan struct{}
	mu      sync.Mutex
}

func newAttachmentBudget() *attachmentBudget {
	return &attachmentBudget{limit: preparedAttachmentBudgetBytes, changed: make(chan struct{})}
}

func (budget *attachmentBudget) acquire(ctx context.Context, size int64) error {
	if size < 0 || size > budget.limit {
		return fmt.Errorf("prepared attachments exceed the %d MiB campaign memory budget", budget.limit>>20)
	}
	for size > 0 {
		budget.mu.Lock()
		if budget.used+size <= budget.limit {
			budget.used += size
			budget.mu.Unlock()
			return nil
		}
		changed := budget.changed
		budget.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	return nil
}

func (budget *attachmentBudget) release(size int64) {
	if size <= 0 {
		return
	}
	budget.mu.Lock()
	budget.used -= size
	if budget.used < 0 {
		budget.mu.Unlock()
		panic("attachment budget released more bytes than reserved")
	}
	close(budget.changed)
	budget.changed = make(chan struct{})
	budget.mu.Unlock()
}
