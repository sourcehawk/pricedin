// Package health serves the liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger checks that a dependency answers. *pgxpool.Pool implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Liveness answers 200 with {"status":"ok"} for as long as the process
// serves HTTP. It checks no dependency.
func Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, "application/json", http.StatusOK, status{Status: "ok"})
	})
}

// Readiness answers 200 with {"status":"ok"} when db answers a ping within
// timeout. Otherwise it answers 503 with an RFC 9457 problem document, and
// logs the cause on logger. The response never contains the cause, because a
// connection error can name hosts and credentials.
func Readiness(db Pinger, timeout time.Duration, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			logger.WarnContext(r.Context(), "readiness check failed", slog.String("dependency", "database"), slog.Any("error", err))
			writeJSON(w, "application/problem+json", http.StatusServiceUnavailable, problem{
				Type:   "about:blank",
				Title:  http.StatusText(http.StatusServiceUnavailable),
				Status: http.StatusServiceUnavailable,
				Detail: "The database is not reachable.",
			})
			return
		}
		writeJSON(w, "application/json", http.StatusOK, status{Status: "ok"})
	})
}

type status struct {
	Status string `json:"status"`
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func writeJSON(w http.ResponseWriter, contentType string, code int, body any) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	// The status line is already sent, so a failed write has no one to tell.
	_ = json.NewEncoder(w).Encode(body)
}
