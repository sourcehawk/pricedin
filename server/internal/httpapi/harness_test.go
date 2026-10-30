package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/sourcehawk/pricedin/server/api/apitest"
	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/auth/authtest"
	"github.com/sourcehawk/pricedin/server/internal/httpapi"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
)

const (
	ownerEmail    = "owner@example.com"
	ownerPassword = "correct horse battery"
	webOrigin     = "https://pricedin.example.com"
	sessionTTL    = 30 * 24 * time.Hour
)

type harness struct {
	handler   http.Handler
	doc       *openapi3.T
	router    routers.Router
	authStore *authtest.Store
	auth      *auth.Service
	data      *stockstest.Store
	loader    *stockstest.Loader
	stocks    *stocks.Service
	clock     *authtest.Clock
}

type option func(*httpapi.Options, *auth.Options)

func withoutOwner() option {
	return func(_ *httpapi.Options, a *auth.Options) { a.OwnerEmail, a.OwnerPasswordHash = "", "" }
}

func withInsecureCookie() option {
	return func(o *httpapi.Options, _ *auth.Options) { o.CookieSecure = false }
}

func withProxyHeader(h string) option {
	return func(o *httpapi.Options, _ *auth.Options) { o.TrustedProxyHeader = h }
}

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	doc := apitest.Load(t)
	// The document's server is relative; requests in the tests go to this
	// host.
	doc.Servers = openapi3.Servers{{URL: "http://pricedin.example.com"}}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	phc, err := auth.HashPassword(ownerPassword, auth.Params{Memory: 64, Time: 1, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		doc: doc, router: router, authStore: authtest.NewStore(), data: stockstest.Fixture(t),
		loader: &stockstest.Loader{}, clock: authtest.NewClock(stockstest.Now),
	}
	ao := auth.Options{OwnerEmail: ownerEmail, OwnerPasswordHash: phc, SessionTTL: sessionTTL, Now: h.clock.Now}
	o := httpapi.Options{
		Logger:       slog.New(slog.DiscardHandler),
		Origins:      auth.NewOrigins([]string{webOrigin}),
		CookieSecure: true,
		Now:          h.clock.Now,
	}
	for _, opt := range opts {
		opt(&o, &ao)
	}
	if h.auth, err = auth.New(h.authStore, ao); err != nil {
		t.Fatal(err)
	}
	h.stocks = stocks.New(h.data, stocks.Options{Now: h.clock.Now, Loader: h.loader})
	o.Auth, o.Stocks = h.auth, h.stocks
	h.handler = httpapi.New(o)
	return h
}

// request builds a request with a JSON body when body is not nil.
func request(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var r io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, "http://pricedin.example.com"+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "192.0.2.10:4444"
	return req
}

type response struct {
	*http.Response
	body []byte
}

func (r response) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// problem decodes a problem document and checks its status and type.
func (r response) problem(t *testing.T, status int, typ string) map[string]any {
	t.Helper()
	if r.StatusCode != status {
		t.Fatalf("status = %d, want %d; body %s", r.StatusCode, status, r.body)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var p map[string]any
	r.json(t, &p)
	if p["type"] != "urn:pricedin:problem:"+typ || p["status"] != float64(status) {
		t.Errorf("problem = %v, want type %s and status %d", p, typ, status)
	}
	if p["requestId"] == "" || p["requestId"] != r.Header.Get("X-Request-ID") {
		t.Errorf("requestId = %v, want the X-Request-ID header %q", p["requestId"], r.Header.Get("X-Request-ID"))
	}
	return p
}

// do serves req and validates the response, and the request too when
// validRequest is set, against the OpenAPI document.
func (h *harness) do(t *testing.T, req *http.Request, validRequest bool) response {
	t.Helper()
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	route, params, err := h.router.FindRoute(req)
	if err != nil {
		if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s has no documented route but answered %d", req.Method, req.URL.Path, res.StatusCode)
		}
		return response{res, body}
	}
	// The route lookup does not read the body; validation does.
	req.Body = io.NopCloser(bytes.NewReader(reqBody))
	in := &openapi3filter.RequestValidationInput{
		Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, MultiError: true},
	}
	if validRequest {
		if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
			t.Errorf("request %s %s does not match the document: %v", req.Method, req.URL, err)
		}
	}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in, Status: res.StatusCode, Header: res.Header,
		Options: &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	}
	out.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
		t.Errorf("response %d of %s %s does not match the document: %v\nbody: %.2000s", res.StatusCode, req.Method, req.URL.Path, err, body)
	}
	return response{res, body}
}

// login logs in and returns the session cookie.
func (h *harness) login(t *testing.T) *http.Cookie {
	t.Helper()
	req := request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": ownerEmail, "password": ownerPassword})
	req.Header.Set("Origin", webOrigin)
	res := h.do(t, req, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body %s", res.StatusCode, res.body)
	}
	for _, c := range res.Cookies() {
		if c.Name == "pricedin_session" {
			return c
		}
	}
	t.Fatal("login set no session cookie")
	return nil
}

// token creates an API token.
func (h *harness) token(t *testing.T) string {
	t.Helper()
	token, _, err := h.auth.CreateAPIToken(context.Background(), "test-"+strings.ReplaceAll(t.Name(), "/", "-"))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// withBearer and withCookie add credentials to a request.
func withBearer(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func withCookie(req *http.Request, c *http.Cookie) *http.Request {
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	return req
}

func readCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}
