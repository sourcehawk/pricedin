//go:build integration

package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestTokenCreateListAndRevoke(t *testing.T) {
	env := map[string]string{"PRICEDIN_DATABASE_URL": pgtest.NewURL(t)}

	// run shows standard error too; the token is the last line.
	lines := strings.Split(strings.TrimSpace(run(t, env, "auth", "token", "create", "agent-1")), "\n")
	token := lines[len(lines)-1]
	if len(token) != 43 {
		t.Fatalf("create printed %q last, want the 43-character token", token)
	}
	out := run(t, env, "auth", "token", "list")
	if !strings.Contains(out, "agent-1") || !strings.Contains(out, "active") || strings.Contains(out, token) {
		t.Errorf("list:\n%s\nwant agent-1 as active, without the token", out)
	}
	if _, err := runErr(t, env, "auth", "token", "create", "agent-1"); err == nil {
		t.Error("a second create with the same name succeeded")
	}
	run(t, env, "auth", "token", "revoke", "agent-1")
	if out := run(t, env, "auth", "token", "list"); !strings.Contains(out, "revoked") {
		t.Errorf("list after revoke:\n%s\nwant agent-1 revoked", out)
	}
	if _, err := runErr(t, env, "auth", "token", "revoke", "agent-1"); err == nil {
		t.Error("revoking a revoked token succeeded")
	}
}

func TestSessionsRevokeAll(t *testing.T) {
	url := pgtest.NewURL(t)
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st := store.New(pool)
	now := time.Now()
	if err := st.InsertSession(t.Context(), store.Session{TokenHash: []byte("hash"), CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PRICEDIN_DATABASE_URL": url}
	if out := run(t, env, "auth", "sessions", "revoke-all"); !strings.Contains(out, "ended 1 sessions") {
		t.Errorf("revoke-all printed %q, want ended 1 sessions", out)
	}
	if _, err := st.GetSession(t.Context(), []byte("hash")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetSession() after revoke-all error = %v, want ErrNotFound", err)
	}
}
