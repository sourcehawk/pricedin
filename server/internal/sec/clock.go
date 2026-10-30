package sec

import (
	"context"
	"time"
)

// Clock is the time source of a Client. Tests pass a fake one to prove the
// rate limit and the retry waits without sleeping.
type Clock interface {
	Now() time.Time
	// SleepUntil returns at t, or earlier with ctx.Err() when ctx ends.
	SleepUntil(ctx context.Context, t time.Time) error
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) SleepUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
