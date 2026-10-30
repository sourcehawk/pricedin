package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/health"
)

type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func serve(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func TestLivenessAlwaysReportsOK(t *testing.T) {
	resp := serve(t, health.Liveness(), "/healthz")
	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if got := resp.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if diff := cmp.Diff(map[string]any{"status": "ok"}, decode(t, resp)); diff != "" {
		t.Errorf("body mismatch (-want +got):\n%s", diff)
	}
}

func TestReadinessReportsOKWhenTheDatabaseAnswers(t *testing.T) {
	ok := pingFunc(func(context.Context) error { return nil })
	resp := serve(t, health.Readiness(ok, time.Second, discard()), "/readyz")
	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if diff := cmp.Diff(map[string]any{"status": "ok"}, decode(t, resp)); diff != "" {
		t.Errorf("body mismatch (-want +got):\n%s", diff)
	}
}

func TestReadinessReportsProblemWhenThePingFails(t *testing.T) {
	failing := pingFunc(func(context.Context) error {
		return errors.New("dial tcp 10.0.0.5:5432: connect: connection refused (password=secret)")
	})
	resp := serve(t, health.Readiness(failing, time.Second, discard()), "/readyz")
	assertProblem(t, resp)
}

func TestReadinessGivesUpAfterTheTimeout(t *testing.T) {
	var deadline time.Time
	hanging := pingFunc(func(ctx context.Context) error {
		deadline, _ = ctx.Deadline()
		<-ctx.Done()
		return ctx.Err()
	})
	start := time.Now()
	resp := serve(t, health.Readiness(hanging, 20*time.Millisecond, discard()), "/readyz")
	assertProblem(t, resp)
	if deadline.IsZero() || deadline.Sub(start) > time.Second {
		t.Errorf("ping deadline = %v after the start, want about 20ms", deadline.Sub(start))
	}
}

func assertProblem(t *testing.T, resp *httptest.ResponseRecorder) {
	t.Helper()
	if resp.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}
	if got := resp.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	raw := resp.Body.Bytes()
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "10.0.0.5") {
		t.Errorf("body leaks the ping error: %s", raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := map[string]any{
		"type":   "about:blank",
		"title":  "Service Unavailable",
		"status": float64(http.StatusServiceUnavailable),
		"detail": "The database is not reachable.",
	}
	if diff := cmp.Diff(want, body); diff != "" {
		t.Errorf("problem mismatch (-want +got):\n%s", diff)
	}
}
