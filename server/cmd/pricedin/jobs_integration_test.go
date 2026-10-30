//go:build integration

package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/fixtureserver"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

// fixtureEnv returns the settings of a process that uses a fresh database
// and an in-process fixture server.
func fixtureEnv(t *testing.T) map[string]string {
	t.Helper()
	h, err := fixtureserver.New(fixtureserver.Options{Root: "../../../testdata", MassiveAPIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return map[string]string{
		"PRICEDIN_DATABASE_URL":       pgtest.NewURL(t),
		"PRICEDIN_BLOB_DIR":           t.TempDir(),
		"PRICEDIN_SEC_USER_AGENT":     "PricedIn test@example.com",
		"PRICEDIN_SEC_BASE_URL":       srv.URL + fixtureserver.PrefixSECWWW,
		"PRICEDIN_SEC_DATA_BASE_URL":  srv.URL + fixtureserver.PrefixSECData,
		"PRICEDIN_MASSIVE_BASE_URL":   srv.URL + fixtureserver.PrefixMassive,
		"PRICEDIN_MASSIVE_API_KEY":    "test-key",
		"PRICEDIN_MASSIVE_RATE_LIMIT": "1000",
		"PRICEDIN_UNIVERSE":           "NVDA,KO",
	}
}

func TestJobsRunAndIngestCompanyAgainstTheFixtureServer(t *testing.T) {
	env := fixtureEnv(t)

	out := run(t, env, "jobs", "run", "sec-tickers")
	var tickers map[string]int64
	if err := json.Unmarshal([]byte(lastLine(out)), &tickers); err != nil || tickers["Tickers"] != 13 {
		t.Errorf("jobs run sec-tickers printed %q (%v), want the counts with 13 tickers", lastLine(out), err)
	}
	out = run(t, env, "jobs", "run", "universe")
	if !strings.Contains(out, `"Symbol":"KO"`) || !strings.Contains(out, `"Symbol":"NVDA"`) || strings.Contains(out, `"AAPL"`) {
		t.Errorf("jobs run universe printed %q, want KO and NVDA only", out)
	}
	out = run(t, env, "ingest", "company", "KO")
	if !strings.Contains(out, `"Symbol":"KO"`) {
		t.Errorf("ingest company KO printed %q, want its counts", out)
	}
	out = run(t, env, "jobs", "list")
	if !strings.Contains(out, "company") || !strings.Contains(out, "succeeded") {
		t.Errorf("jobs list:\n%s\nwant the company run as succeeded", out)
	}
	if _, err := runErr(t, env, "jobs", "run", "sec-company"); err == nil {
		t.Error("jobs run sec-company without a ticker: want a usage error")
	}
}

// lastLine returns the last line of out that is not a log line.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], `"level"`) {
			return lines[i]
		}
	}
	return ""
}
