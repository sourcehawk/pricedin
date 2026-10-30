package httpserver_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sourcehawk/pricedin/server/internal/httpserver"
)

// seenID serves the request ID it finds in the context.
var seenID = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(httpserver.RequestIDFrom(r.Context())))
})

func TestRequestIDMakesAnIDAndSendsIt(t *testing.T) {
	rec := httptest.NewRecorder()
	httpserver.RequestID(seenID).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	id := rec.Header().Get("X-Request-ID")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Errorf("X-Request-ID = %q, want 16 hex digits", id)
	}
	if rec.Body.String() != id {
		t.Errorf("context ID = %q, want the header's %q", rec.Body.String(), id)
	}
}

func TestRequestIDKeepsAClientIDThatIsSafe(t *testing.T) {
	tests := map[string]bool{
		"abc-123_X.y":           true,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		"has space":             false,
		"new\nline":             false,
		"quote\"":               false,
		"":                      false,
	}
	for id, kept := range tests {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		r.Header.Set("X-Request-ID", id)
		rec := httptest.NewRecorder()
		httpserver.RequestID(seenID).ServeHTTP(rec, r)
		if got := rec.Header().Get("X-Request-ID"); (got == id) != kept {
			t.Errorf("client ID %q gave %q, want kept = %v", id, got, kept)
		}
	}
}

func TestRequestIDTwiceKeepsTheFirstID(t *testing.T) {
	rec := httptest.NewRecorder()
	httpserver.RequestID(httpserver.RequestID(seenID)).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	if rec.Body.String() != rec.Header().Get("X-Request-ID") {
		t.Errorf("inner ID %q differs from the header %q", rec.Body.String(), rec.Header().Get("X-Request-ID"))
	}
}

func TestObserveLogsTheRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := httpserver.RequestID(httpserver.Observe(logger, prometheus.NewRegistry())(seenID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["request_id"] != rec.Header().Get("X-Request-ID") {
		t.Errorf("request_id = %v, want %q", line["request_id"], rec.Header().Get("X-Request-ID"))
	}
}
