//go:build integration

package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func TestIngestRunLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	id, err := s.StartIngestRun(ctx, "sec-daily")
	if err != nil {
		t.Fatalf("StartIngestRun() error = %v", err)
	}
	run, err := s.GetIngestRun(ctx, id)
	if err != nil {
		t.Fatalf("GetIngestRun() error = %v", err)
	}
	if run.Job != "sec-daily" || run.Status != store.RunRunning || run.FinishedAt != nil || run.StartedAt.IsZero() {
		t.Errorf("started run = %+v, want a running sec-daily run with a start time", run)
	}

	stats := map[string]int64{"facts_added": 120, "companies": 3}
	if err := s.FinishIngestRun(ctx, id, store.RunSucceeded, stats, ""); err != nil {
		t.Fatalf("FinishIngestRun() error = %v", err)
	}
	run, err = s.GetIngestRun(ctx, id)
	if err != nil {
		t.Fatalf("GetIngestRun() error = %v", err)
	}
	if run.Status != store.RunSucceeded || run.FinishedAt == nil || run.FinishedAt.Before(run.StartedAt) {
		t.Errorf("finished run = %+v, want succeeded with a finish time after the start", run)
	}
	if diff := cmp.Diff(stats, run.Stats); diff != "" {
		t.Errorf("Stats (-want +got):\n%s", diff)
	}

	err = s.FinishIngestRun(ctx, id, store.RunFailed, nil, "again")
	wantNotFound(t, "FinishIngestRun() of a finished run", err)
	_, err = s.GetIngestRun(ctx, id+100)
	wantNotFound(t, "GetIngestRun(unknown)", err)
}

func TestFinishIngestRunRejectsTheRunningStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	id, err := s.StartIngestRun(ctx, "sec-daily")
	if err != nil {
		t.Fatalf("StartIngestRun() error = %v", err)
	}
	if err := s.FinishIngestRun(ctx, id, store.RunRunning, nil, ""); err == nil {
		t.Error("FinishIngestRun(running) error = nil, want an error")
	}
}

func TestListLatestIngestRunsGivesTheNewestRunOfEachJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustStart := func(job string) int64 {
		t.Helper()
		id, err := s.StartIngestRun(ctx, job)
		if err != nil {
			t.Fatalf("StartIngestRun(%s) error = %v", job, err)
		}
		return id
	}
	old := mustStart("prices-daily")
	if err := s.FinishIngestRun(ctx, old, store.RunFailed, nil, "provider down"); err != nil {
		t.Fatalf("FinishIngestRun() error = %v", err)
	}
	newest := mustStart("prices-daily")
	failed := mustStart("sec-daily")
	if err := s.FinishIngestRun(ctx, failed, store.RunFailed, map[string]int64{"companies": 1}, "boom"); err != nil {
		t.Fatalf("FinishIngestRun() error = %v", err)
	}

	runs, err := s.ListLatestIngestRuns(ctx)
	if err != nil {
		t.Fatalf("ListLatestIngestRuns() error = %v", err)
	}
	type summary struct {
		ID     int64
		Job    string
		Status store.RunStatus
		Error  string
	}
	got := make([]summary, len(runs))
	for i, r := range runs {
		got[i] = summary{r.ID, r.Job, r.Status, r.Error}
	}
	want := []summary{
		{newest, "prices-daily", store.RunRunning, ""},
		{failed, "sec-daily", store.RunFailed, "boom"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListLatestIngestRuns() (-want +got):\n%s", diff)
	}
}

func TestSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	sess := store.Session{
		TokenHash: hash("token-1"), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), LastSeenAt: now,
		UserAgent: "Firefox", IP: "192.0.2.10", OwnerFingerprint: hash("owner"),
	}
	expired := store.Session{TokenHash: hash("token-2"), CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), LastSeenAt: now.Add(-48 * time.Hour)}

	for _, x := range []store.Session{sess, expired} {
		if err := s.InsertSession(ctx, x); err != nil {
			t.Fatalf("InsertSession() error = %v", err)
		}
	}
	if err := s.InsertSession(ctx, sess); !errors.Is(err, store.ErrConflict) {
		t.Errorf("InsertSession() of a stored hash error = %v, want ErrConflict", err)
	}

	got, err := s.GetSession(ctx, sess.TokenHash)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if diff := cmp.Diff(sess, got); diff != "" {
		t.Errorf("GetSession() (-want +got):\n%s", diff)
	}

	seen := now.Add(time.Minute)
	if err := s.UpdateSessionLastSeen(ctx, sess.TokenHash, seen); err != nil {
		t.Fatalf("UpdateSessionLastSeen() error = %v", err)
	}
	got, err = s.GetSession(ctx, sess.TokenHash)
	if err != nil || !got.LastSeenAt.Equal(seen) {
		t.Errorf("LastSeenAt = %v, %v; want %v", got.LastSeenAt, err, seen)
	}
	err = s.UpdateSessionLastSeen(ctx, hash("missing"), seen)
	wantNotFound(t, "UpdateSessionLastSeen(unknown)", err)

	n, err := s.DeleteExpiredSessions(ctx, now)
	if err != nil || n != 1 {
		t.Errorf("DeleteExpiredSessions() = %d, %v; want 1, nil", n, err)
	}
	_, err = s.GetSession(ctx, expired.TokenHash)
	wantNotFound(t, "GetSession(expired)", err)

	if err := s.DeleteSession(ctx, sess.TokenHash); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	_, err = s.GetSession(ctx, sess.TokenHash)
	wantNotFound(t, "GetSession() after DeleteSession()", err)
	if err := s.DeleteSession(ctx, sess.TokenHash); err != nil {
		t.Errorf("second DeleteSession() error = %v, want nil", err)
	}
}

func TestDeleteAllSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, token := range []string{"a", "b"} {
		if err := s.InsertSession(ctx, store.Session{TokenHash: hash(token), CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteAllSessions(ctx)
	if err != nil || n != 2 {
		t.Errorf("DeleteAllSessions() = %d, %v; want 2, nil", n, err)
	}
	_, err = s.GetSession(ctx, hash("a"))
	wantNotFound(t, "GetSession() after DeleteAllSessions()", err)
}

func TestAPITokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	laptop, err := s.InsertAPIToken(ctx, "laptop", hash("secret-1"), now)
	if err != nil {
		t.Fatalf("InsertAPIToken() error = %v", err)
	}
	want := store.APIToken{ID: laptop.ID, Name: "laptop", TokenHash: hash("secret-1"), CreatedAt: now}
	if diff := cmp.Diff(want, laptop); diff != "" {
		t.Errorf("InsertAPIToken() (-want +got):\n%s", diff)
	}
	agent, err := s.InsertAPIToken(ctx, "agent", hash("secret-2"), now)
	if err != nil {
		t.Fatalf("InsertAPIToken(agent) error = %v", err)
	}
	if _, err := s.InsertAPIToken(ctx, "laptop", hash("secret-3"), now); !errors.Is(err, store.ErrConflict) {
		t.Errorf("InsertAPIToken() with a used name error = %v, want ErrConflict", err)
	}
	if _, err := s.InsertAPIToken(ctx, "other", hash("secret-1"), now); !errors.Is(err, store.ErrConflict) {
		t.Errorf("InsertAPIToken() with a used hash error = %v, want ErrConflict", err)
	}

	used := now.Add(time.Hour)
	if err := s.UpdateAPITokenLastUsed(ctx, laptop.ID, used); err != nil {
		t.Fatalf("UpdateAPITokenLastUsed() error = %v", err)
	}
	got, err := s.GetActiveAPIToken(ctx, hash("secret-1"))
	if err != nil {
		t.Fatalf("GetActiveAPIToken() error = %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Errorf("LastUsedAt = %v, want %v", got.LastUsedAt, used)
	}
	err = s.UpdateAPITokenLastUsed(ctx, agent.ID+100, used)
	wantNotFound(t, "UpdateAPITokenLastUsed(unknown)", err)

	revoked := now.Add(2 * time.Hour)
	if err := s.RevokeAPIToken(ctx, "laptop", revoked); err != nil {
		t.Fatalf("RevokeAPIToken() error = %v", err)
	}
	_, err = s.GetActiveAPIToken(ctx, hash("secret-1"))
	wantNotFound(t, "GetActiveAPIToken() of a revoked token", err)
	err = s.RevokeAPIToken(ctx, "laptop", revoked)
	wantNotFound(t, "RevokeAPIToken() of a revoked token", err)
	_, err = s.GetActiveAPIToken(ctx, hash("never-issued"))
	wantNotFound(t, "GetActiveAPIToken(unknown)", err)

	list, err := s.ListAPITokens(ctx)
	if err != nil {
		t.Fatalf("ListAPITokens() error = %v", err)
	}
	wantList := []store.APIToken{
		{ID: laptop.ID, Name: "laptop", TokenHash: hash("secret-1"), CreatedAt: now, LastUsedAt: &used, RevokedAt: &revoked},
		{ID: agent.ID, Name: "agent", TokenHash: hash("secret-2"), CreatedAt: now},
	}
	if diff := cmp.Diff(wantList, list); diff != "" {
		t.Errorf("ListAPITokens() (-want +got):\n%s", diff)
	}
}
