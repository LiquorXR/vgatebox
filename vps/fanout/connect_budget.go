package main

import (
	"context"
	"sync"
)

// One adjustable budget covers temporary probes and production dial attempts.
// A smaller limit drains existing work; it never interrupts established exits.
type connectBudget struct {
	mu                               sync.Mutex
	limit, active, productionWaiting int
	wake                             chan struct{}
}

func newConnectBudget(limit int) *connectBudget {
	return &connectBudget{limit: limit, wake: make(chan struct{})}
}
func (b *connectBudget) signal() { close(b.wake); b.wake = make(chan struct{}) }
func (b *connectBudget) setLimit(limit int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.limit = limit
	b.signal()
}

// limitValue reports the configured parallelism for read-only callers such as
// batch-duration estimation. It never changes the budget.
func (b *connectBudget) limitValue() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit < 1 {
		return 1
	}
	return b.limit
}
func (b *connectBudget) acquire(ctx context.Context, production bool) (func(), error) {
	b.mu.Lock()
	if production {
		b.productionWaiting++
		b.signal()
	}
	for {
		if err := ctx.Err(); err != nil {
			if production {
				b.productionWaiting--
				b.signal()
			}
			b.mu.Unlock()
			return nil, err
		}
		if b.active < b.limit && (production || b.productionWaiting == 0) {
			if production {
				b.productionWaiting--
			}
			b.active++
			b.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { b.mu.Lock(); b.active--; b.signal(); b.mu.Unlock() }) }, nil
		}
		wake := b.wake
		b.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-wake:
		}
		b.mu.Lock()
	}
}
