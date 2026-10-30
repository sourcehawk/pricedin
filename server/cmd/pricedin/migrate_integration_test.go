//go:build integration

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// run executes the command and returns what it printed.
func run(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(func(k string) string { return env[k] }, &out)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("pricedin %s error = %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func TestMigrateUpDownAndUpAgain(t *testing.T) {
	env := map[string]string{"PRICEDIN_DATABASE_URL": pgtest.NewEmptyURL(t)}

	out := run(t, env, "migrate", "status")
	if strings.Contains(out, "applied") || !strings.Contains(out, "pending  00001_sec_reference.sql") {
		t.Errorf("status before up:\n%s\nwant every migration pending", out)
	}

	out = run(t, env, "migrate", "up")
	if !strings.Contains(out, "up  00005_operations.sql") {
		t.Errorf("up:\n%s\nwant the last migration applied", out)
	}
	out = run(t, env, "migrate", "status")
	if strings.Contains(out, "pending") {
		t.Errorf("status after up:\n%s\nwant no pending migration", out)
	}

	out = run(t, env, "migrate", "down")
	if !strings.Contains(out, "down  00005_operations.sql") || strings.Contains(out, "00004") {
		t.Errorf("down:\n%s\nwant only the last migration rolled back", out)
	}
	out = run(t, env, "migrate", "down", "--to", "0")
	if !strings.Contains(out, "down  00001_sec_reference.sql") {
		t.Errorf("down --to 0:\n%s\nwant every migration rolled back", out)
	}
	out = run(t, env, "migrate", "status")
	if strings.Contains(out, "applied") {
		t.Errorf("status after down --to 0:\n%s\nwant every migration pending", out)
	}

	run(t, env, "migrate", "up")
	out = run(t, env, "migrate", "up")
	if !strings.Contains(out, "no pending migrations") {
		t.Errorf("second up:\n%s\nwant no pending migrations", out)
	}
}
