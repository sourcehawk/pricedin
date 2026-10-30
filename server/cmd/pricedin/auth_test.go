package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/config"
)

func executeWithInput(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd(func(string) string { return "" }, &out)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestHashPasswordFromStdinPrintsAVerifyingHash(t *testing.T) {
	out, err := executeWithInput(t, "correct horse battery\n", "auth", "hash-password", "--stdin")
	if err != nil {
		t.Fatalf("hash-password error = %v", err)
	}
	phc := strings.TrimSpace(out)
	h, err := auth.ParseHash(phc)
	if err != nil {
		t.Fatalf("output %q is not a hash: %v", out, err)
	}
	if !h.Verify("correct horse battery") {
		t.Error("the hash does not verify the password")
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("output %q, want the hash on one line and nothing else", out)
	}
}

func TestHashPasswordKeepsSpacesButNotTheLineEnd(t *testing.T) {
	out, err := executeWithInput(t, "  spaced out password  \r\n", "auth", "hash-password", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := auth.ParseHash(strings.TrimSpace(out))
	if !h.Verify("  spaced out password  ") {
		t.Error("the hash does not verify the password with its spaces")
	}
}

func TestHashPasswordRejectsAShortPassword(t *testing.T) {
	_, err := executeWithInput(t, "short\n", "auth", "hash-password", "--stdin")
	if !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("error = %v, want ErrWeakPassword", err)
	}
}

func TestHashPasswordNeedsATerminalOrStdin(t *testing.T) {
	_, err := executeWithInput(t, "correct horse battery\n", "auth", "hash-password")
	if err == nil || !strings.Contains(err.Error(), "--stdin") {
		t.Errorf("error = %v, want a hint to use --stdin", err)
	}
}

func TestTokenCommandsRequireTheDatabase(t *testing.T) {
	for _, args := range [][]string{{"auth", "token", "create", "x"}, {"auth", "token", "list"}, {"auth", "token", "revoke", "x"}} {
		_, err := executeWithInput(t, "", args...)
		if !errors.Is(err, config.ErrMissing) {
			t.Errorf("%v error = %v, want a missing database URL", args, err)
		}
	}
}
