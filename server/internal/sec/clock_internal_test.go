package sec

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSystemClockSleepsUntilTheTime(t *testing.T) {
	var c systemClock
	start := time.Now()
	if err := c.SleepUntil(t.Context(), start.Add(20*time.Millisecond)); err != nil {
		t.Fatalf("SleepUntil() error = %v", err)
	}
	if waited := time.Since(start); waited < 20*time.Millisecond {
		t.Errorf("waited %v, want at least 20ms", waited)
	}
}

func TestSystemClockReturnsAtOnceForAPastTime(t *testing.T) {
	var c systemClock
	if err := c.SleepUntil(t.Context(), time.Now().Add(-time.Hour)); err != nil {
		t.Errorf("SleepUntil() error = %v", err)
	}
}

func TestSystemClockStopsWhenTheContextEnds(t *testing.T) {
	var c systemClock
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.SleepUntil(ctx, start.Add(time.Hour))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("SleepUntil() did not return when the context ended")
	}
}

func TestBackoffStaysWithinItsLimit(t *testing.T) {
	for attempt := 1; attempt <= 100; attempt++ {
		for range 20 {
			if d := backoff(attempt); d < firstBackoff/2 || d > maxBackoff {
				t.Fatalf("backoff(%d) = %v, want between %v and %v", attempt, d, firstBackoff/2, maxBackoff)
			}
		}
	}
}
