package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func TestJobsRunRejectsAnUnknownJob(t *testing.T) {
	err := execute(t, nil, "jobs", "run", "nope")
	if err == nil || !strings.Contains(err.Error(), `unknown job "nope"`) {
		t.Errorf("jobs run nope error = %v, want an unknown job", err)
	}
}

// Each command checks the settings of the services it calls before it
// connects to anything.
func TestCommandsRequireTheSettingsOfTheServicesTheyCall(t *testing.T) {
	db := map[string]string{"PRICEDIN_DATABASE_URL": "postgres://127.0.0.1:1/pricedin"}
	tests := []struct {
		args     []string
		env      map[string]string
		wantVars []string
	}{
		{[]string{"jobs", "run", "sec-tickers"}, db, []string{"PRICEDIN_SEC_USER_AGENT"}},
		{[]string{"jobs", "run", "prices-daily"}, db, []string{"PRICEDIN_MASSIVE_API_KEY"}},
		{[]string{"jobs", "run", "daily"}, db, []string{"PRICEDIN_SEC_USER_AGENT", "PRICEDIN_MASSIVE_API_KEY"}},
		{[]string{"ingest", "company", "NVDA"}, db, []string{"PRICEDIN_SEC_USER_AGENT", "PRICEDIN_MASSIVE_API_KEY"}},
		{[]string{"scheduler"}, db, []string{"PRICEDIN_SEC_USER_AGENT", "PRICEDIN_MASSIVE_API_KEY"}},
		{[]string{"jobs", "run", "universe"}, nil, []string{"PRICEDIN_DATABASE_URL"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			err := execute(t, tt.env, tt.args...)
			if !errors.Is(err, config.ErrMissing) {
				t.Fatalf("error = %v, want a missing setting", err)
			}
			for _, v := range tt.wantVars {
				if !strings.Contains(err.Error(), v) {
					t.Errorf("error = %v, want it to name %s", err, v)
				}
			}
		})
	}
}

func TestJobsListWithoutADatabaseListsEveryJob(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd(func(string) string { return "" }, &out)
	cmd.SetArgs([]string{"jobs", "list"})
	cmd.SetOut(&out)
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("jobs list error = %v", err)
	}
	for _, job := range []string{
		"sec-tickers", "sec-bulk", "sec-company", "sec-daily", "price-reference",
		"prices-backfill", "prices-daily", "fundamentals-rebuild", "company", "daily", "universe",
	} {
		if !strings.Contains(out.String(), job) {
			t.Errorf("jobs list does not list %s:\n%s", job, out.String())
		}
	}
}
