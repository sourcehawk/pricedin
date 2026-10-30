package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// catchUpAge is the age of the last successful daily run from which the
// scheduler runs the daily job at once when it starts.
const catchUpAge = 24 * time.Hour

// Scheduler runs a job once a day at a fixed UTC time.
type Scheduler struct {
	// At is the time of day in UTC, as an offset from midnight.
	At time.Duration
	// Run runs the job. Its error is logged; the next run still comes.
	Run func(ctx context.Context) error
	// LastSuccess returns the end of the job's last successful run, and
	// false when it never succeeded.
	LastSuccess func(ctx context.Context) (time.Time, bool, error)
	// Now and Sleep are the clock. Sleep returns early with ctx.Err() when
	// ctx ends. Defaults: time.Now and a timer.
	Now    func() time.Time
	Sleep  func(ctx context.Context, d time.Duration) error
	Logger *slog.Logger
}

// NextRun returns the first moment after now at the time of day at, in
// UTC.
func NextRun(now time.Time, at time.Duration) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(at)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// NeedsCatchUp reports whether a scheduler that starts at now runs the job
// at once: when the job never succeeded (ok false), or its last success
// ended more than a day before now.
func NeedsCatchUp(last time.Time, ok bool, now time.Time) bool {
	return !ok || now.Sub(last) > catchUpAge
}

// Loop runs the job at start when NeedsCatchUp says so, and then at every
// NextRun, until ctx ends. A run that is under way when ctx ends gets the
// ended context, so it stops at its next check. Loop returns nil after ctx
// ended, and an error only when LastSuccess fails.
func (s *Scheduler) Loop(ctx context.Context) error {
	now, sleep, logger := s.Now, s.Sleep, s.Logger
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepFor
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	last, ok, err := s.LastSuccess(ctx)
	if err != nil {
		return err
	}
	if NeedsCatchUp(last, ok, now()) {
		logger.InfoContext(ctx, "scheduler: the last successful run is more than a day old; running now")
		s.runOnce(ctx, logger)
	}
	for ctx.Err() == nil {
		next := NextRun(now(), s.At)
		logger.InfoContext(ctx, "scheduler: next run", "at", next)
		// The sleep fails only when ctx ends, which the loop checks.
		_ = sleep(ctx, next.Sub(now()))
		if ctx.Err() == nil {
			s.runOnce(ctx, logger)
		}
	}
	logger.InfoContext(ctx, "scheduler: stopped")
	return nil
}

func (s *Scheduler) runOnce(ctx context.Context, logger *slog.Logger) {
	if err := s.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.ErrorContext(ctx, "scheduler: run failed", "error", err.Error())
	}
}

func sleepFor(ctx context.Context, d time.Duration) error {
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
