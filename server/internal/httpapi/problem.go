package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/sourcehawk/pricedin/server/internal/httpserver"
)

// problemPrefix starts the type URI of every problem the API writes.
const problemPrefix = "urn:pricedin:problem:"

// problemKind is one entry of the ProblemType enum of the OpenAPI document.
type problemKind struct {
	code   string
	status int
	title  string
}

var (
	invalidRequest       = problemKind{"invalid-request", http.StatusBadRequest, "Invalid request"}
	invalidTicker        = problemKind{"invalid-ticker", http.StatusBadRequest, "Invalid ticker"}
	invalidLevers        = problemKind{"invalid-levers", http.StatusBadRequest, "Lever out of bounds"}
	invalidCredentials   = problemKind{"invalid-credentials", http.StatusUnauthorized, "Invalid email or password"}
	unauthenticated      = problemKind{"unauthenticated", http.StatusUnauthorized, "Not authenticated"}
	forbiddenOrigin      = problemKind{"forbidden-origin", http.StatusForbidden, "Origin not allowed"}
	unknownTicker        = problemKind{"unknown-ticker", http.StatusNotFound, "Unknown ticker"}
	notFound             = problemKind{"not-found", http.StatusNotFound, "Not found"}
	methodNotAllowed     = problemKind{"method-not-allowed", http.StatusMethodNotAllowed, "Method not allowed"}
	notLoaded            = problemKind{"not-loaded", http.StatusConflict, "Company data not loaded"}
	missingInput         = problemKind{"missing-input", http.StatusUnprocessableEntity, "Missing input"}
	bodyTooLarge         = problemKind{"body-too-large", http.StatusRequestEntityTooLarge, "Request body too large"}
	unsupportedMediaType = problemKind{"unsupported-media-type", http.StatusUnsupportedMediaType, "JSON required"}
	rateLimited          = problemKind{"rate-limited", http.StatusTooManyRequests, "Too many login attempts"}
	loginDisabled        = problemKind{"login-disabled", http.StatusServiceUnavailable, "Login not configured"}
	loadUnavailable      = problemKind{"load-unavailable", http.StatusNotImplemented, "Loading not available"}
	loadFailed           = problemKind{"load-failed", http.StatusBadGateway, "Loading failed"}
	internal             = problemKind{"internal", http.StatusInternalServerError, "Internal error"}
)

// problem is an RFC 9457 problem document with the API's extensions.
type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	Loadable  *bool  `json:"loadable,omitempty"`
	Input     string `json:"input,omitempty"`
}

// writeProblem writes a problem of kind. detail is for a person and must
// not hold internals; edit, when not nil, sets extension fields.
func writeProblem(w http.ResponseWriter, r *http.Request, kind problemKind, detail string, edit func(*problem)) {
	p := problem{
		Type: problemPrefix + kind.code, Title: kind.title, Status: kind.status, Detail: detail,
		Instance: r.URL.Path, RequestID: httpserver.RequestIDFrom(r.Context()),
	}
	if edit != nil {
		edit(&p)
	}
	body, _ := json.Marshal(p) // a problem always marshals
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(kind.status)
	_, _ = w.Write(append(body, '\n'))
}

// writeInternal logs err and writes an internal problem without it.
func writeInternal(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	logger.ErrorContext(r.Context(), "request failed",
		slog.String("method", r.Method), slog.String("path", r.URL.Path),
		slog.String("request_id", httpserver.RequestIDFrom(r.Context())), slog.Any("error", err))
	writeProblem(w, r, internal, "An unexpected error occurred. The request ID identifies it in the server logs.", nil)
}

// writeJSON writes v as JSON with status 200.
func writeJSON(logger *slog.Logger, w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeInternal(logger, w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}
