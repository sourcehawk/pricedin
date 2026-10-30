//go:build integration

package main

import (
	"strings"
	"testing"

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
