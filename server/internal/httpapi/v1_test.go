package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/api"
	"github.com/sourcehawk/pricedin/server/internal/auth/authtest"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
	"github.com/sourcehawk/pricedin/server/internal/httpapi"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// v1Path fills the path parameters of a route pattern.
func v1Path(pattern string) string {
	return strings.ReplaceAll(pattern, "{ticker}", "NVDA")
}

func TestTheRoutesAreTheDocumentedOperations(t *testing.T) {
	h := newHarness(t)
	var documented []string
	for path, item := range h.doc.Paths.Map() {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		for method := range item.Operations() {
			documented = append(documented, method+" "+path)
		}
	}
	served := httpapi.Routes()
	slices.Sort(documented)
	slices.Sort(served)
	if diff := cmp.Diff(documented, served); diff != "" {
		t.Errorf("served routes differ from the document (-documented +served):\n%s", diff)
	}
}

func TestEveryV1RouteNeedsCredentials(t *testing.T) {
	h := newHarness(t)
	for _, route := range httpapi.Routes() {
		method, pattern, _ := strings.Cut(route, " ")
		if !strings.HasPrefix(pattern, "/api/v1/") {
			continue
		}
		t.Run(route, func(t *testing.T) {
			for name, req := range map[string]*http.Request{
				"no credentials": request(t, method, v1Path(pattern), map[string]any{}),
				"unknown token":  withBearer(request(t, method, v1Path(pattern), map[string]any{}), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
				"unknown cookie": withCookie(request(t, method, v1Path(pattern), map[string]any{}), &http.Cookie{Name: "pricedin_session", Value: "nope"}),
				"basic auth":     withAuthorization(request(t, method, v1Path(pattern), map[string]any{}), "Basic b3duZXI6cHc="),
				"empty bearer":   withAuthorization(request(t, method, v1Path(pattern), map[string]any{}), "Bearer "),
			} {
				req.Header.Set("Origin", webOrigin)
				res := h.do(t, req, false)
				res.problem(t, http.StatusUnauthorized, "unauthenticated")
				if res.Header.Get("WWW-Authenticate") == "" {
					t.Errorf("%s: no WWW-Authenticate header", name)
				}
			}
		})
	}
}

func TestUnknownV1PathsNeedCredentialsToo(t *testing.T) {
	h := newHarness(t)
	h.do(t, request(t, http.MethodGet, "/api/v1/secret", nil), false).problem(t, http.StatusUnauthorized, "unauthenticated")
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/secret", nil), h.token(t)), false).problem(t, http.StatusNotFound, "not-found")
}

func withAuthorization(req *http.Request, v string) *http.Request {
	req.Header.Set("Authorization", v)
	return req
}

func TestBearerTokenAuthenticatesUntilRevoked(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	if res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/status", nil), token), true); res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, res.body)
	}
	if err := h.auth.RevokeAPIToken(context.Background(), "test-"+t.Name()); err != nil {
		t.Fatal(err)
	}
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/status", nil), token), true).problem(t, http.StatusUnauthorized, "unauthenticated")
}

func TestAnInvalidBearerTokenDoesNotFallBackToTheCookie(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	req := withBearer(withCookie(request(t, http.MethodGet, "/api/v1/status", nil), c), "wrong")
	res := h.do(t, req, false)
	res.problem(t, http.StatusUnauthorized, "unauthenticated")
	if len(res.Cookies()) != 0 {
		t.Errorf("cookies = %v, want the session cookie kept: the bearer token failed, not the session", res.Cookies())
	}
	if res := h.do(t, withCookie(request(t, http.MethodGet, "/api/v1/status", nil), c), true); res.StatusCode != http.StatusOK {
		t.Errorf("the session after a bad bearer token: status %d, want 200", res.StatusCode)
	}
}

// deadlineStore records whether the token lookup had a deadline.
type deadlineStore struct {
	*authtest.Store
	deadline time.Duration
}

func (d *deadlineStore) GetActiveAPIToken(ctx context.Context, hash []byte) (store.APIToken, error) {
	if at, ok := ctx.Deadline(); ok {
		d.deadline = time.Until(at)
	}
	return d.Store.GetActiveAPIToken(ctx, hash)
}

func TestCredentialLookupsHaveADeadline(t *testing.T) {
	as := &deadlineStore{Store: authtest.NewStore()}
	h := newHarnessOn(t, as, stockstest.Fixture(t))
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/status", nil), "unknown"), true).problem(t, http.StatusUnauthorized, "unauthenticated")
	if as.deadline <= 0 || as.deadline > 15*time.Second {
		t.Errorf("token lookup deadline = %v away, want at most 15s", as.deadline)
	}
}

func TestCookieWritesNeedTheWebAppsOrigin(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	for name, origin := range map[string]string{"no origin": "", "foreign origin": "https://evil.example.com"} {
		t.Run(name, func(t *testing.T) {
			req := withCookie(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", map[string]any{}), c)
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			h.do(t, req, false).problem(t, http.StatusForbidden, "forbidden-origin")
		})
	}
	req := withCookie(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", map[string]any{}), c)
	req.Header.Set("Origin", webOrigin)
	if res := h.do(t, req, true); res.StatusCode != http.StatusOK {
		t.Errorf("with the origin: status = %d, body %.500s", res.StatusCode, res.body)
	}
	if res := h.do(t, withCookie(request(t, http.MethodGet, "/api/v1/stocks/KO", nil), c), true); res.StatusCode != http.StatusOK {
		t.Errorf("a GET without Origin: status = %d, want 200", res.StatusCode)
	}
}

func TestBearerWritesNeedNoOrigin(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", map[string]any{}), h.token(t)), true)
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, body %.500s", res.StatusCode, res.body)
	}
}

func TestWritesNeedJSON(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	for name, ct := range map[string]string{"none": "", "form": "application/x-www-form-urlencoded", "text": "text/plain", "multipart": "multipart/form-data; boundary=x"} {
		t.Run(name, func(t *testing.T) {
			req := withBearer(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", nil), token)
			req.Body = readCloser("{}")
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			h.do(t, req, false).problem(t, http.StatusUnsupportedMediaType, "unsupported-media-type")
		})
	}
}

func TestLargeBodiesAreRefused(t *testing.T) {
	h := newHarness(t)
	req := withBearer(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", nil), h.token(t))
	req.Header.Set("Content-Type", "application/json")
	req.Body = readCloser(`{"scenarios":[` + strings.Repeat(`{"name":"x","growth":0.1,"exitPe":10},`, 3000) + `{"name":"x","growth":0.1,"exitPe":10}]}`)
	h.do(t, req, false).problem(t, http.StatusRequestEntityTooLarge, "body-too-large")
}

func TestWrongMethodAnswers405WithAllow(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, withBearer(request(t, http.MethodDelete, "/api/v1/stocks/NVDA", nil), h.token(t)), false)
	res.problem(t, http.StatusMethodNotAllowed, "method-not-allowed")
	if allow := res.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q, want GET", allow)
	}
}

func TestAPanicAnswers500WithoutInternals(t *testing.T) {
	h := httpapi.Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret database password")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/status", http.NoBody))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("status %d, body %s; want 500 without the panic value", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "urn:pricedin:problem:internal") {
		t.Errorf("body %s, want an internal problem", rec.Body)
	}
}

func TestOpenAPIDocumentIsServed(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/openapi.yaml", nil), h.token(t)), true)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/yaml" || !bytes.Equal(res.body, api.Document()) {
		t.Errorf("status %d, type %q; want 200 with the document as application/yaml", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestSearch(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/search?q=nv&limit=5", nil), token), true)
	var got struct {
		Results []stocks.SearchResult `json:"results"`
	}
	res.json(t, &got)
	if len(got.Results) != 1 || got.Results[0].Symbol != "NVDA" || !got.Results[0].Loaded {
		t.Errorf("results = %+v, want NVDA, loaded", got.Results)
	}
	// The contract counts the length of q in characters, not bytes.
	if res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/search?q="+url.QueryEscape(strings.Repeat("é", 100)), nil), token), true); res.StatusCode != http.StatusOK {
		t.Errorf("q of 100 two-byte characters: status %d, want 200", res.StatusCode)
	}
	for _, bad := range []string{"/api/v1/search?q=" + url.QueryEscape(strings.Repeat("é", 101)), "/api/v1/search", "/api/v1/search?q=a&limit=0", "/api/v1/search?q=a&limit=51", "/api/v1/search?q=a&limit=x", "/api/v1/search?q=" + strings.Repeat("a", 101)} {
		h.do(t, withBearer(request(t, http.MethodGet, bad, nil), token), false).problem(t, http.StatusBadRequest, "invalid-request")
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/status", nil), h.token(t)), true)
	var got map[string]any
	res.json(t, &got)
	if got["latestPriceDate"] != "2026-10-29" {
		t.Errorf("status = %v, want latestPriceDate 2026-10-29", got)
	}
}

func TestProfiles(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	for ticker, loaded := range map[string]bool{"NVDA": true, "ko": true, "MSFT": false} {
		res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/"+ticker, nil), token), true)
		var p map[string]any
		res.json(t, &p)
		if res.StatusCode != http.StatusOK || p["loaded"] != loaded {
			t.Errorf("%s: status %d, loaded %v; want 200, loaded %v", ticker, res.StatusCode, p["loaded"], loaded)
		}
	}
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/ZZZZ", nil), token), true).problem(t, http.StatusNotFound, "unknown-ticker")
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/A_B", nil), token), false).problem(t, http.StatusBadRequest, "invalid-ticker")
}

func TestHistory(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	for _, rng := range []string{"", "?range=6M", "?range=1Y", "?range=3Y", "?range=max"} {
		res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/NVDA/history"+rng, nil), token), true)
		if res.StatusCode != http.StatusOK {
			t.Errorf("range %q: status %d", rng, res.StatusCode)
		}
	}
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/NVDA/history?range=2Y", nil), token), false).problem(t, http.StatusBadRequest, "invalid-request")
	p := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/MSFT/history", nil), token), true).problem(t, http.StatusConflict, "not-loaded")
	if p["loadable"] != true {
		t.Errorf("problem = %v, want loadable true", p)
	}
}

func TestFundamentals(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	for _, ticker := range []string{"NVDA", "KO"} {
		if res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/"+ticker+"/fundamentals", nil), token), true); res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", ticker, res.StatusCode)
		}
	}
	h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/MSFT/fundamentals", nil), token), true).problem(t, http.StatusConflict, "not-loaded")
}

func TestEvaluationEqualsTheEngineForTheSameInputs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res := h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/NVDA/evaluation", map[string]any{"targetReturn": 0.12}), h.token(t)), true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %.500s", res.StatusCode, res.body)
	}
	var got struct {
		Levers     engine.Levers   `json:"levers"`
		Evaluation json.RawMessage `json:"evaluation"`
		Sources    map[string]struct {
			Source map[string]any `json:"source"`
			AsOf   string         `json:"asOf"`
		} `json:"sources"`
	}
	res.json(t, &got)
	hist, err := h.stocks.History(ctx, "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	want, err := engine.Evaluate(hist.History, got.Levers)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	var a, b any
	_ = json.Unmarshal(wantJSON, &a)
	_ = json.Unmarshal(got.Evaluation, &b)
	if diff := cmp.Diff(a, b); diff != "" {
		t.Errorf("evaluation differs from engine.Evaluate (-engine +api):\n%.3000s", diff)
	}
	if got.Levers.TargetReturn != 0.12 {
		t.Errorf("levers.targetReturn = %v, want 0.12", got.Levers.TargetReturn)
	}
	for _, input := range []string{"price", "ttmEps", "dividend"} {
		s, ok := got.Sources[input]
		if !ok || s.Source["kind"] == nil || s.AsOf == "" {
			t.Errorf("sources.%s = %+v, want a source with an as-of date", input, s)
		}
	}
}

func TestEvaluationOfEveryLeverMode(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	bodies := map[string]map[string]any{
		"exit P/E today, baseline today": {"exitPe": map[string]any{"mode": "today"}, "baseline": map[string]any{"mode": "today"}, "chartRange": "6M"},
		"trailing both":                  {"exitPe": map[string]any{"mode": "trailing", "trailingMonths": 24}, "baseline": map[string]any{"mode": "trailing", "trailingMonths": 36}, "chartRange": "1Y"},
		"custom year by year":            {"exitPe": map[string]any{"mode": "custom", "custom": 25}, "baseline": map[string]any{"mode": "custom", "custom": map[string]any{"yearly": true, "rates": []float64{0.3, 0.2, 0.1}}}},
		"custom flat with cases":         {"baseline": map[string]any{"mode": "custom", "custom": map[string]any{"rate": 0.08}}, "scenarios": []map[string]any{{"name": "Bear", "growth": 0.02, "exitPe": 12}, {"name": "Base", "growth": 0.08, "exitPe": 18}, {"name": "Bull", "growth": 0.15, "exitPe": 25}}},
		"negative EPS":                   {"ttmEps": -1.5, "price": 50},
		"zero EPS, long hold":            {"ttmEps": 0, "holdingYears": 10, "dividend": 1},
		"dividends reach the target":     {"price": 10, "dividend": 5, "targetReturn": 0.05},
	}
	for _, ticker := range []string{"NVDA", "KO"} {
		for name, body := range bodies {
			t.Run(ticker+"/"+name, func(t *testing.T) {
				res := h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/"+ticker+"/evaluation", body), token), true)
				if res.StatusCode != http.StatusOK {
					t.Errorf("status = %d, body %.500s", res.StatusCode, res.body)
				}
			})
		}
	}
}

func TestEvaluationRequestExamplesWork(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	op := h.doc.Paths.Find("/api/v1/stocks/{ticker}/evaluation").Post
	for name, ex := range op.RequestBody.Value.Content.Get("application/json").Examples {
		t.Run(name, func(t *testing.T) {
			res := h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", ex.Value.Value), token), true)
			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, body %.500s", res.StatusCode, res.body)
			}
		})
	}
}

func TestEvaluationErrors(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	post := func(ticker, body string) response {
		req := withBearer(request(t, http.MethodPost, "/api/v1/stocks/"+ticker+"/evaluation", nil), token)
		req.Header.Set("Content-Type", "application/json")
		req.Body = readCloser(body)
		return h.do(t, req, false)
	}
	p := post("KO", `{"targetReturn": 2}`).problem(t, http.StatusBadRequest, "invalid-levers")
	if d, _ := p["detail"].(string); !strings.Contains(d, "targetReturn") || strings.Contains(d, "stocks:") {
		t.Errorf("detail = %q, want the lever's bound without internals", d)
	}
	post("KO", `{"exitPe": {"mode": "custom"}}`).problem(t, http.StatusBadRequest, "invalid-levers")
	// The custom exit P/E has its bounds in every mode, since the levers echo it.
	post("KO", `{"exitPe": {"mode": "estimated", "custom": 5000}}`).problem(t, http.StatusBadRequest, "invalid-levers")
	post("KO", `{"target": 0.1}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `{"exitPe": {"mode": "estimated", "extra": 1}}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `{"holdingYears": "3"}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `{} {}`).problem(t, http.StatusBadRequest, "invalid-request")
	// No field of a request body is nullable, so null is never a value.
	post("KO", `{"baseline": {"mode": "custom", "custom": {"yearly": true, "rates": [0.3, null, 0.1]}}}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `{"price": null}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `{"scenarios": [{"name": "Bear", "growth": null, "exitPe": 12}]}`).problem(t, http.StatusBadRequest, "invalid-request")
	post("KO", `null`).problem(t, http.StatusBadRequest, "invalid-request")
	post("ZZZZ", `{}`).problem(t, http.StatusNotFound, "unknown-ticker")
	post("MSFT", `{}`).problem(t, http.StatusConflict, "not-loaded")
	if res := post("KO", ``); res.StatusCode != http.StatusOK {
		t.Errorf("empty body: status %d, want 200 with the defaults", res.StatusCode)
	}

	h.data.Bars = nil
	p = post("KO", `{}`).problem(t, http.StatusUnprocessableEntity, "missing-input")
	if p["input"] != "price" {
		t.Errorf("problem = %v, want input price", p)
	}
}

func TestLoad(t *testing.T) {
	h := newHarness(t)
	token := h.token(t)
	load := func(ticker string) response {
		return h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/"+ticker+"/load", map[string]any{}), token), true)
	}
	if res := load("msft"); res.StatusCode != http.StatusOK || !slices.Equal(h.loader.Symbols, []string{"MSFT"}) {
		t.Errorf("status %d, loaded %v; want 200 after loading MSFT", res.StatusCode, h.loader.Symbols)
	}
	load("ZZZZ").problem(t, http.StatusNotFound, "unknown-ticker")
	h.loader.Err = errors.New("sec: 503 from data.sec.gov")
	p := load("MSFT").problem(t, http.StatusBadGateway, "load-failed")
	if d, _ := p["detail"].(string); strings.Contains(d, "data.sec.gov") {
		t.Errorf("detail %q leaks the cause", p["detail"])
	}
	h.loader.Err = stocks.ErrLoaderUnavailable
	load("MSFT").problem(t, http.StatusNotImplemented, "load-unavailable")
}

func TestLoadWithAnEmptyBody(t *testing.T) {
	h := newHarness(t)
	req := withBearer(request(t, http.MethodPost, "/api/v1/stocks/MSFT/load", nil), h.token(t))
	req.Header.Set("Content-Type", "application/json")
	if res := h.do(t, req, true); res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
}

func TestUnknownAPIPathIsAProblem(t *testing.T) {
	h := newHarness(t)
	h.do(t, request(t, http.MethodGet, "/api/nothing", nil), false).problem(t, http.StatusNotFound, "not-found")
}

// Stored data never makes an evaluation fail: a TTM EPS too small for the
// engine and a close above its bounds still give 200, with reasons.
func TestStoredDataOutsideTheEngineBoundsAnswers200(t *testing.T) {
	h := newHarness(t)
	for i, v := range []float64{-0.2, -0.2, -0.12995} {
		end := []string{"2026-04-03", "2026-07-03", "2026-10-02"}[i]
		d, _ := domain.ParseDate(end)
		h.data.SetQuarterValue(t, stockstest.CIKKO, "EarningsPerShareDiluted", d, v)
	}
	today, _ := domain.ParseDate("2026-10-30")
	h.data.Bars = append(h.data.Bars, domain.PriceBar{Symbol: "KO", Date: today, Close: 2e6, Source: "massive"})
	res := h.do(t, withBearer(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", map[string]any{}), h.token(t)), true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.StatusCode, res.body)
	}
	var got struct {
		Levers     engine.Levers     `json:"levers"`
		Evaluation engine.Evaluation `json:"evaluation"`
	}
	res.json(t, &got)
	if got.Levers.TTMEPS != 0 || got.Levers.Price != 81.38 || got.Evaluation.Required.Growth.Reason != engine.ReasonNeedsPositiveEPS {
		t.Errorf("ttmEps %v, price %v, required %+v; want 0, 81.38 and needs_positive_eps", got.Levers.TTMEPS, got.Levers.Price, got.Evaluation.Required.Growth)
	}
}
