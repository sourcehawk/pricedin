package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

type requestIDKey struct{}

// RequestIDHeader carries the request ID in both directions.
const RequestIDHeader = "X-Request-ID"

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID gives each request an ID: the client's X-Request-ID when it is
// 1 to 64 letters, digits, '.', '_' or '-', else 16 random hex digits. It
// puts the ID in the request's context and in the response's X-Request-ID
// header. Inside another RequestID it keeps the outer ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) != "" {
			next.ServeHTTP(w, r)
			return
		}
		id := r.Header.Get(RequestIDHeader)
		if !safeRequestID.MatchString(id) {
			b := make([]byte, 8)
			_, _ = rand.Read(b) // crypto/rand.Read never fails
			id = hex.EncodeToString(b)
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom returns the request ID that RequestID put in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
