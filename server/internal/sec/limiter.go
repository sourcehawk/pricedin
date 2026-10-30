package sec

import (
	"context"
	"sync"
	"time"
)

// limiter is a token bucket that holds one token and refills it once every
// interval. A larger bucket would let a burst plus the refill exceed the rate
// within one second, and SEC counts requests in any one-second window.
type limiter struct {
	clock    Clock
	interval time.Duration

	mu   sync.Mutex
	next time.Time // the earliest time of the next request
}

// wait returns when the caller may send one request, or with ctx.Err() when
// ctx ends first. Each call takes its own slot, so concurrent callers queue.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.clock.Now()
	slot := l.next
	if slot.Before(now) {
		slot = now
	}
	l.next = slot.Add(l.interval)
	l.mu.Unlock()

	if !slot.After(now) {
		return ctx.Err()
	}
	return l.clock.SleepUntil(ctx, slot)
}
