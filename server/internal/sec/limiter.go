package sec

import (
	"context"
	"sync"
	"time"
)

// limiter is a token bucket that holds one token and refills it interval
// after each admission. A larger bucket would let a burst plus the refill
// exceed the rate within one second, and SEC counts requests in any
// one-second window.
type limiter struct {
	clock    Clock
	interval time.Duration

	mu   sync.Mutex
	next time.Time // the earliest time of the next admission
}

// wait returns when the caller may send one request, or with ctx.Err() when
// ctx ends first. Concurrent callers can be admitted in a different order
// than they called.
func (l *limiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := l.clock.Now()
		if !now.Before(l.next) {
			l.next = now.Add(l.interval)
			l.mu.Unlock()
			return ctx.Err()
		}
		wake := l.next
		l.mu.Unlock()
		// A waiter can wake late, together with other waiters, so it checks
		// the time again under the lock instead of trusting its wake time.
		if err := l.clock.SleepUntil(ctx, wake); err != nil {
			return err
		}
	}
}
