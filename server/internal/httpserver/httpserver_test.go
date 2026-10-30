package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sourcehawk/pricedin/server/internal/httpserver"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func listen(t *testing.T) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return l
}

func get(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return http.DefaultClient.Do(req)
}

// reachable reports whether a GET to url gets any HTTP response.
func reachable(t *testing.T, url string) bool {
	t.Helper()
	resp, err := get(t, url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func TestNewServerSetsTimeouts(t *testing.T) {
	srv := httpserver.NewServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("timeouts = header %v, read %v, write %v, idle %v; want all positive",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

func TestRunServesUntilTheContextEnds(t *testing.T) {
	l := listen(t)
	srv := httpserver.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- httpserver.Run(ctx, discard(), httpserver.Unit{Name: "api", Server: srv, Listener: l}) }()

	resp, err := get(t, "http://"+l.Addr().String()+"/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil after the context ends", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after the context ended")
	}
	if reachable(t, "http://"+l.Addr().String()+"/") {
		t.Error("GET after shutdown succeeded, want a connection error")
	}
}

func TestRunFinishesRequestsInFlight(t *testing.T) {
	l := listen(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httpserver.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	}))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- httpserver.Run(ctx, discard(), httpserver.Unit{Name: "api", Server: srv, Listener: l}) }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+l.Addr().String()+"/", http.NoBody)
		if err != nil {
			got <- result{err: err}
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			got <- result{err: err}
			return
		}
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got <- result{body: string(b), err: err}
	}()

	<-entered
	cancel()
	close(release)

	r := <-got
	if r.err != nil || r.body != "done" {
		t.Errorf("request in flight = (%q, %v), want (%q, nil)", r.body, r.err, "done")
	}
	if err := <-done; err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestRunStopsEveryUnitWhenOneFails(t *testing.T) {
	healthy := listen(t)
	broken := listen(t)
	_ = broken.Close()

	done := make(chan error, 1)
	go func() {
		done <- httpserver.Run(t.Context(), discard(),
			httpserver.Unit{Name: "api", Server: httpserver.NewServer(http.NotFoundHandler()), Listener: healthy},
			httpserver.Unit{Name: "metrics", Server: httpserver.NewServer(http.NotFoundHandler()), Listener: broken},
		)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "metrics") {
			t.Errorf("Run() error = %v, want an error that names the metrics unit", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after a unit failed")
	}
	if reachable(t, "http://"+healthy.Addr().String()+"/") {
		t.Error("the healthy unit still serves, want it shut down")
	}
}

func TestObserveLogsEachRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stocks/{ticker}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "12345")
	})
	h := httpserver.Observe(logger, prometheus.NewRegistry())(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stocks/GRMN", http.NoBody))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q is not one JSON object: %v", buf.String(), err)
	}
	want := map[string]any{
		"msg":    "http request",
		"method": "GET",
		"path":   "/stocks/GRMN",
		"route":  "GET /stocks/{ticker}",
		"status": float64(http.StatusTeapot),
		"bytes":  float64(5),
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log field %q = %#v, want %#v", k, line[k], v)
		}
	}
	if _, ok := line["duration"]; !ok {
		t.Error("log line has no duration field")
	}
}

func TestObserveCountsRequestsByRoute(t *testing.T) {
	reg := prometheus.NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stocks/{ticker}", func(http.ResponseWriter, *http.Request) {})
	h := httpserver.Observe(discard(), reg)(mux)

	for _, path := range []string{"/stocks/GRMN", "/stocks/KO", "/nowhere"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
	}

	want := `
# HELP pricedin_http_requests_total HTTP requests by method, route pattern and status code.
# TYPE pricedin_http_requests_total counter
pricedin_http_requests_total{code="200",method="GET",route="GET /stocks/{ticker}"} 2
pricedin_http_requests_total{code="404",method="GET",route="unmatched"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "pricedin_http_requests_total"); err != nil {
		t.Error(err)
	}
	if n, err := testutil.GatherAndCount(reg, "pricedin_http_request_duration_seconds"); err != nil || n != 2 {
		t.Errorf("duration histogram series = %d (%v), want 2", n, err)
	}
}

func TestObserveGroupsNonstandardMethods(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := httpserver.Observe(discard(), reg)(http.NotFoundHandler())

	for _, method := range []string{"BREW", "X-RANDOM-1", "X-RANDOM-2", http.MethodDelete} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), method, "/", http.NoBody))
	}

	want := `
# HELP pricedin_http_requests_total HTTP requests by method, route pattern and status code.
# TYPE pricedin_http_requests_total counter
pricedin_http_requests_total{code="404",method="DELETE",route="unmatched"} 1
pricedin_http_requests_total{code="404",method="OTHER",route="unmatched"} 3
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "pricedin_http_requests_total"); err != nil {
		t.Error(err)
	}
	if n, err := testutil.GatherAndCount(reg, "pricedin_http_request_duration_seconds"); err != nil || n != 2 {
		t.Errorf("duration histogram series = %d (%v), want 2", n, err)
	}
}

func TestObserveRecordsTheFinalStatus(t *testing.T) {
	tests := []struct {
		name  string
		codes []int
		want  int
	}{
		{"early hints before the final status", []int{http.StatusEarlyHints, http.StatusOK}, http.StatusOK},
		{"two informational statuses", []int{http.StatusContinue, http.StatusEarlyHints, http.StatusCreated}, http.StatusCreated},
		{"switching protocols is final", []int{http.StatusSwitchingProtocols}, http.StatusSwitchingProtocols},
		{"a second final status is ignored", []int{http.StatusAccepted, http.StatusInternalServerError}, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			reg := prometheus.NewRegistry()
			h := httpserver.Observe(slog.New(slog.NewJSONHandler(&buf, nil)), reg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for _, code := range tt.codes {
					w.WriteHeader(code)
				}
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("log line %q is not one JSON object: %v", buf.String(), err)
			}
			if line["status"] != float64(tt.want) {
				t.Errorf("logged status = %v, want %d", line["status"], tt.want)
			}
			want := fmt.Sprintf(`
# HELP pricedin_http_requests_total HTTP requests by method, route pattern and status code.
# TYPE pricedin_http_requests_total counter
pricedin_http_requests_total{code="%d",method="GET",route="unmatched"} 1
`, tt.want)
			if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "pricedin_http_requests_total"); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestObserveKeepsTheResponseControllerWorking(t *testing.T) {
	h := httpserver.Observe(discard(), prometheus.NewRegistry())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("Flush() error = %v", err)
		} else if err != nil {
			t.Error("Flush() is not supported through the middleware")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
}
