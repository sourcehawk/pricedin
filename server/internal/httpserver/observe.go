package httpserver

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Observe returns middleware that logs one line per request on logger and
// records the pricedin_http_requests_total counter and the
// pricedin_http_request_duration_seconds histogram on reg. Both are labeled
// with the route pattern of the http.ServeMux that served the request, or
// "unmatched", never with the raw path. Observe panics if reg already holds
// these metrics, so call it once per registry.
func Observe(logger *slog.Logger, reg prometheus.Registerer) func(http.Handler) http.Handler {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pricedin_http_requests_total",
		Help: "HTTP requests by method, route pattern and status code.",
	}, []string{"method", "route", "code"})
	durations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pricedin_http_request_duration_seconds",
		Help:    "HTTP request duration by method and route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
	reg.MustRegister(requests, durations)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)

			// The ServeMux sets r.Pattern on the request it routes.
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			method := methodLabel(r.Method)
			requests.WithLabelValues(method, route, strconv.Itoa(rec.status)).Inc()
			durations.WithLabelValues(method, route).Observe(elapsed.Seconds())
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("route", route),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.bytes),
				slog.Duration("duration", elapsed),
			)
		})
	}
}

// methodLabel returns OTHER for a method outside RFC 9110 and RFC 5789.
// The server accepts any token as a method, so the raw value would let a
// client create any number of metric series.
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the Flusher and Hijacker of the
// underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
