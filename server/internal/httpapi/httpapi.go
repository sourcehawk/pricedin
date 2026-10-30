// Package httpapi serves the PricedIn HTTP API under /api: the owner's
// login under /api/auth and the data under /api/v1, as server/api/openapi.yaml
// describes them. Every /api/v1 request needs a session cookie or a bearer
// token; every error is an RFC 9457 problem document.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/httpserver"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
)

// Options configure the API.
type Options struct {
	Logger *slog.Logger
	Auth   *auth.Service
	Stocks *stocks.Service
	// Origins are the web app's origins. A login, a logout, and a
	// state-changing request with the session cookie must come from one.
	Origins auth.Origins
	// CookieSecure sets the Secure attribute of the session cookie.
	CookieSecure bool
	// TrustedProxyHeader names the header with the client address, as
	// auth.ClientIP reads it; empty means the connection's address.
	TrustedProxyHeader string
	// Now returns the current time for the login rate limit; nil means
	// time.Now.
	Now func() time.Time
}

const (
	// CookieName is the name of the session cookie.
	CookieName = "pricedin_session"
	// maxBodyBytes bounds every request body.
	maxBodyBytes = 64 << 10
	// requestTimeout bounds a request's work; loadTimeout a company load.
	requestTimeout = 15 * time.Second
	loadTimeout    = 2 * time.Minute
	// loginAttempts logins per loginWindow per client address.
	loginAttempts = 5
	loginWindow   = time.Minute
)

type api struct {
	Options
	limiter *auth.Limiter
}

// route is one operation of the OpenAPI document.
type route struct {
	method, pattern string
	handler         http.HandlerFunc
	// timeout replaces requestTimeout, for a long operation.
	timeout time.Duration
}

func (a *api) authRoutes() []route {
	return []route{
		{method: http.MethodPost, pattern: "/api/auth/login", handler: a.login},
		{method: http.MethodPost, pattern: "/api/auth/logout", handler: a.logout},
		{method: http.MethodGet, pattern: "/api/auth/session", handler: a.session},
	}
}

func (a *api) v1Routes() []route {
	return []route{
		{method: http.MethodGet, pattern: "/api/v1/openapi.yaml", handler: a.openAPI},
		{method: http.MethodGet, pattern: "/api/v1/search", handler: a.search},
		{method: http.MethodGet, pattern: "/api/v1/status", handler: a.status},
		{method: http.MethodGet, pattern: "/api/v1/stocks/{ticker}", handler: a.profile},
		{method: http.MethodGet, pattern: "/api/v1/stocks/{ticker}/history", handler: a.history},
		{method: http.MethodGet, pattern: "/api/v1/stocks/{ticker}/fundamentals", handler: a.fundamentals},
		{method: http.MethodPost, pattern: "/api/v1/stocks/{ticker}/evaluation", handler: a.evaluate},
		{method: http.MethodPost, pattern: "/api/v1/stocks/{ticker}/load", handler: a.load, timeout: loadTimeout},
	}
}

// New returns the handler of every path below /api/. Mount it at "/api/".
func New(o Options) http.Handler {
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &api{Options: o, limiter: auth.NewLimiter(loginAttempts, loginWindow, o.Now)}

	root := http.NewServeMux()
	register(root, a.authRoutes())
	root.HandleFunc("/api/", a.notFound)

	// Every path below /api/v1/, also an unknown one, passes authenticate
	// first, so no route can be served without credentials.
	v1 := http.NewServeMux()
	register(v1, a.v1Routes())
	v1.HandleFunc("/api/v1/", a.notFound)
	root.Handle("/api/v1/", a.authenticate(v1))

	return httpserver.RequestID(Recover(o.Logger)(common(root)))
}

// register adds the routes to mux, each with its timeout, the JSON rule
// for writes, and a 405 problem for the other methods of its path.
func register(mux *http.ServeMux, routes []route) {
	allowed := map[string][]string{}
	for _, r := range routes {
		timeout := r.timeout
		if timeout == 0 {
			timeout = requestTimeout
		}
		h := withTimeout(timeout, r.handler)
		if isWrite(r.method) {
			h = requireJSON(h)
		}
		mux.Handle(r.method+" "+r.pattern, h)
		if !slices.Contains(allowed[r.pattern], r.method) {
			allowed[r.pattern] = append(allowed[r.pattern], r.method)
		}
	}
	for pattern, methods := range allowed {
		if slices.Contains(methods, http.MethodGet) {
			methods = append(methods, http.MethodHead)
		}
		allow := strings.Join(methods, ", ")
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			writeProblem(w, r, methodNotAllowed, fmt.Sprintf("Use %s.", allow), nil)
		})
	}
}

// common sets the headers of every response and bounds the body.
func common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func withTimeout(d time.Duration, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next(w, r.WithContext(ctx))
	})
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// requireJSON refuses a body that is not declared as JSON. A browser
// cannot send JSON to another site without a CORS preflight, which this
// API never grants, so the rule also stops cross-site form posts.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeProblem(w, r, unsupportedMediaType, "Send Content-Type: application/json.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Recover turns a panic into an internal problem, unless the response has
// started, and logs the panic with its stack.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tw := &trackingWriter{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // the sentinel is panicked as is
					panic(v)
				}
				logger.ErrorContext(r.Context(), "panic serving request",
					slog.String("path", r.URL.Path), slog.String("request_id", httpserver.RequestIDFrom(r.Context())),
					slog.Any("panic", v), slog.String("stack", string(debug.Stack())))
				if !tw.started {
					writeProblem(w, r, internal, "An unexpected error occurred. The request ID identifies it in the server logs.", nil)
				}
			}()
			next.ServeHTTP(tw, r)
		})
	}
}

type trackingWriter struct {
	http.ResponseWriter
	started bool
}

func (t *trackingWriter) WriteHeader(code int) {
	t.started = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.started = true
	return t.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *trackingWriter) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

func (a *api) notFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, notFound, "No such endpoint.", nil)
}

// authenticate admits a request with an active API token, or with a valid
// session cookie. A request with an Authorization header is judged by its
// token alone. A write with the cookie must also come from a web origin.
func (a *api) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			scheme, token, _ := strings.Cut(h, " ")
			token = strings.TrimSpace(token)
			if !strings.EqualFold(scheme, "Bearer") || token == "" {
				a.unauthenticated(w, r)
				return
			}
			if _, err := a.Auth.APIToken(r.Context(), token); err != nil {
				a.authFailure(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(CookieName)
		if err != nil {
			a.unauthenticated(w, r)
			return
		}
		if _, err := a.Auth.Session(r.Context(), c.Value); err != nil {
			a.authFailure(w, r, err)
			return
		}
		if isWrite(r.Method) && !a.Origins.Allowed(r.Header.Get("Origin")) {
			writeProblem(w, r, forbiddenOrigin, "A request with the session cookie must come from the web app.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) authFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		a.unauthenticated(w, r)
		return
	}
	writeInternal(a.Logger, w, r, err)
}

// unauthenticated writes a 401 problem, and clears a session cookie that
// the request sent, since it is not valid.
func (a *api) unauthenticated(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(CookieName); err == nil {
		http.SetCookie(w, a.cookie("", -1))
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="pricedin"`)
	writeProblem(w, r, unauthenticated, "Log in, or send an API token as Authorization: Bearer <token>.", nil)
}

// cookie returns the session cookie with the value and max age in
// seconds; a negative max age deletes it.
func (a *api) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: CookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
}

// errEmptyBody is returned by decodeJSON for a body without a value.
var errEmptyBody = errors.New("the request body is empty")

// decodeJSON decodes the one JSON value of the body into v and rejects
// unknown fields. A body that is too large gives *http.MaxBytesError.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errEmptyBody
		}
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return errors.New("the request body must hold one JSON value")
	}
	return nil
}

// badBody writes the problem for a decodeJSON error.
func badBody(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(w, r, bodyTooLarge, fmt.Sprintf("The body may hold at most %d bytes.", maxBodyBytes), nil)
		return
	}
	writeProblem(w, r, invalidRequest, strings.TrimPrefix(err.Error(), "json: "), nil)
}
