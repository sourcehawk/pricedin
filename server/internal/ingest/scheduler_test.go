package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/ingest"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextRun(t *testing.T) {
	at := time.Hour + 30*time.Minute
	tests := []struct {
		now, want string
	}{
		{"2026-10-30T00:00:00Z", "2026-10-30T01:30:00Z"},
		{"2026-10-30T01:29:59Z", "2026-10-30T01:30:00Z"},
		{"2026-10-30T01:30:00Z", "2026-10-31T01:30:00Z"},
		{"2026-10-30T23:00:00Z", "2026-10-31T01:30:00Z"},
		// A local time is read in UTC.
		{"2026-10-30T21:00:00-04:00", "2026-10-31T01:30:00Z"},
		{"2026-12-31T12:00:00Z", "2027-01-01T01:30:00Z"},
	}
	for _, tt := range tests {
		if got := ingest.NextRun(utc(tt.now), at); !got.Equal(utc(tt.want)) {
			t.Errorf("NextRun(%s) = %s, want %s", tt.now, got, tt.want)
		}
	}
}

func TestNeedsCatchUp(t *testing.T) {
	now := utc("2026-10-30T12:00:00Z")
	tests := []struct {
		name string
		last time.Time
		ok   bool
		want bool
	}{
		{"never succeeded", time.Time{}, false, true},
		{"succeeded an hour ago", now.Add(-time.Hour), true, false},
		{"succeeded exactly a day ago", now.Add(-24 * time.Hour), true, false},
		{"succeeded more than a day ago", now.Add(-25 * time.Hour), true, true},
	}
	for _, tt := range tests {
		if got := ingest.NeedsCatchUp(tt.last, tt.ok, now); got != tt.want {
			t.Errorf("%s: NeedsCatchUp() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// fakeClock advances only when the scheduler sleeps.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	// stopAfter cancels the loop at the sleep with this number.
	stopAfter int
	cancel    context.CancelFunc
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	if len(c.sleeps) > c.stopAfter {
		c.cancel()
		return ctx.Err()
	}
	c.now = c.now.Add(d)
	return nil
}

func runScheduler(t *testing.T, start, last time.Time, ok bool, sleeps int) (runs []time.Time, clock *fakeClock) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock = &fakeClock{now: start, stopAfter: sleeps, cancel: cancel}
	s := &ingest.Scheduler{
		At: time.Hour + 30*time.Minute,
		Run: func(context.Context) error {
			runs = append(runs, clock.now)
			return errors.New("a failed run does not stop the loop")
		},
		LastSuccess: func(context.Context) (time.Time, bool, error) { return last, ok, nil },
		Now:         clock.Now,
		Sleep:       clock.Sleep,
	}
	if err := s.Loop(ctx); err != nil {
		t.Fatalf("Loop() error = %v", err)
	}
	return runs, clock
}

func TestSchedulerRunsDailyAtTheConfiguredTime(t *testing.T) {
	start := utc("2026-10-30T12:00:00Z")
	runs, clock := runScheduler(t, start, start.Add(-time.Hour), true, 2)
	want := []time.Time{utc("2026-10-31T01:30:00Z"), utc("2026-11-01T01:30:00Z")}
	if diff := cmp.Diff(want, runs); diff != "" {
		t.Errorf("runs (-want +got):\n%s", diff)
	}
	if clock.sleeps[0] != 13*time.Hour+30*time.Minute || clock.sleeps[1] != 24*time.Hour {
		t.Errorf("sleeps = %v, want 13h30m and then 24h", clock.sleeps)
	}
}

func TestSchedulerCatchesUpAtStartWhenTheLastRunIsOld(t *testing.T) {
	start := utc("2026-10-30T12:00:00Z")
	runs, _ := runScheduler(t, start, start.Add(-30*time.Hour), true, 1)
	want := []time.Time{start, utc("2026-10-31T01:30:00Z")}
	if diff := cmp.Diff(want, runs); diff != "" {
		t.Errorf("runs (-want +got):\n%s", diff)
	}
}

func TestSchedulerCatchesUpWhenTheJobNeverSucceeded(t *testing.T) {
	start := utc("2026-10-30T12:00:00Z")
	runs, _ := runScheduler(t, start, time.Time{}, false, 0)
	if diff := cmp.Diff([]time.Time{start}, runs); diff != "" {
		t.Errorf("runs (-want +got):\n%s", diff)
	}
}

func TestSchedulerStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &ingest.Scheduler{
		Run:         func(context.Context) error { t.Error("Run called after the context ended"); return nil },
		LastSuccess: func(context.Context) (time.Time, bool, error) { return time.Now(), true, nil },
	}
	if err := s.Loop(ctx); err != nil {
		t.Errorf("Loop() error = %v, want nil after a shutdown", err)
	}
}

func TestSchedulerReturnsTheErrorOfLastSuccess(t *testing.T) {
	s := &ingest.Scheduler{
		Run:         func(context.Context) error { return nil },
		LastSuccess: func(context.Context) (time.Time, bool, error) { return time.Time{}, false, errors.New("db down") },
	}
	if err := s.Loop(context.Background()); err == nil {
		t.Error("Loop() error = nil, want the LastSuccess error")
	}
}
