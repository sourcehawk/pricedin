package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	contract "github.com/sourcehawk/pricedin/server/api"
	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/engine"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
)

// sessionInfo is the SessionInfo schema.
type sessionInfo struct {
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	if !a.Origins.Allowed(r.Header.Get("Origin")) {
		writeProblem(w, r, forbiddenOrigin, "A login must come from the web app.", nil)
		return
	}
	if ok, wait := a.limiter.Allow(auth.ClientIP(r, a.TrustedProxyHeader)); !ok {
		secs := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeProblem(w, r, rateLimited, fmt.Sprintf("Try again in %d seconds.", secs), nil)
		return
	}
	var body struct {
		Email    *string `json:"email"`
		Password *string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badBody(w, r, err)
		return
	}
	if body.Email == nil || body.Password == nil {
		writeProblem(w, r, invalidRequest, "Send email and password.", nil)
		return
	}
	client := auth.Client{IP: auth.ClientIP(r, a.TrustedProxyHeader), UserAgent: r.UserAgent()}
	token, sess, err := a.Auth.Login(r.Context(), *body.Email, *body.Password, client)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeProblem(w, r, invalidCredentials, "The email or the password is wrong.", nil)
		return
	case errors.Is(err, auth.ErrLoginDisabled):
		writeProblem(w, r, loginDisabled, "This deployment has no owner configured.", nil)
		return
	case err != nil:
		writeInternal(a.Logger, w, r, err)
		return
	}
	http.SetCookie(w, a.cookie(token, int(sess.ExpiresAt.Sub(a.Now()).Seconds())))
	writeJSON(a.Logger, w, r, sessionInfo(sess))
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if !a.Origins.Allowed(r.Header.Get("Origin")) {
		writeProblem(w, r, forbiddenOrigin, "A logout must come from the web app.", nil)
		return
	}
	var body struct{}
	if err := decodeJSON(r, &body); err != nil && !errors.Is(err, errEmptyBody) {
		badBody(w, r, err)
		return
	}
	if c, err := r.Cookie(CookieName); err == nil {
		if err := a.Auth.Logout(r.Context(), c.Value); err != nil {
			writeInternal(a.Logger, w, r, err)
			return
		}
	}
	http.SetCookie(w, a.cookie("", -1))
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) session(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		a.unauthenticated(w, r, false)
		return
	}
	sess, err := a.Auth.Session(r.Context(), c.Value)
	if err != nil {
		a.authFailure(w, r, err, true)
		return
	}
	writeJSON(a.Logger, w, r, sessionInfo(sess))
}

func (a *api) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(contract.Document())
}

// maxQueryLen bounds the search text, in characters.
const maxQueryLen = 100

func (a *api) search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if !query.Has("q") || utf8.RuneCountInString(query.Get("q")) > maxQueryLen {
		writeProblem(w, r, invalidRequest, fmt.Sprintf("Send q, at most %d characters.", maxQueryLen), nil)
		return
	}
	limit := stocks.DefaultSearchLimit
	if s := query.Get("limit"); query.Has("limit") {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > stocks.MaxSearchLimit {
			writeProblem(w, r, invalidRequest, fmt.Sprintf("limit must be 1 to %d.", stocks.MaxSearchLimit), nil)
			return
		}
		limit = n
	}
	results, err := a.Stocks.Search(r.Context(), query.Get("q"), limit)
	if err != nil {
		writeInternal(a.Logger, w, r, err)
		return
	}
	writeJSON(a.Logger, w, r, struct {
		Results []stocks.SearchResult `json:"results"`
	}{results})
}

func (a *api) status(w http.ResponseWriter, r *http.Request) {
	st, err := a.Stocks.Status(r.Context())
	if err != nil {
		writeInternal(a.Logger, w, r, err)
		return
	}
	writeJSON(a.Logger, w, r, st)
}

func (a *api) profile(w http.ResponseWriter, r *http.Request) {
	p, err := a.Stocks.Profile(r.Context(), r.PathValue("ticker"))
	a.respond(w, r, p, err)
}

func (a *api) history(w http.ResponseWriter, r *http.Request) {
	rng, ok := stocks.ParseHistoryRange(r.URL.Query().Get("range"))
	if !ok {
		writeProblem(w, r, invalidRequest, "range must be 6M, 1Y, 3Y or max.", nil)
		return
	}
	h, err := a.Stocks.History(r.Context(), r.PathValue("ticker"), rng)
	a.respond(w, r, h, err)
}

func (a *api) fundamentals(w http.ResponseWriter, r *http.Request) {
	f, err := a.Stocks.Fundamentals(r.Context(), r.PathValue("ticker"))
	a.respond(w, r, f, err)
}

func (a *api) evaluate(w http.ResponseWriter, r *http.Request) {
	var in stocks.LeversInput
	if err := decodeJSON(r, &in); err != nil && !errors.Is(err, errEmptyBody) {
		badBody(w, r, err)
		return
	}
	ev, err := a.Stocks.Evaluate(r.Context(), r.PathValue("ticker"), in)
	a.respond(w, r, ev, err)
}

func (a *api) load(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if err := decodeJSON(r, &body); err != nil && !errors.Is(err, errEmptyBody) {
		badBody(w, r, err)
		return
	}
	// A load outlasts the server's write timeout; a writer that cannot
	// move its deadline keeps the server's.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(loadTimeout + 10*time.Second))
	p, err := a.Stocks.Load(r.Context(), r.PathValue("ticker"))
	switch {
	case errors.Is(err, stocks.ErrLoaderUnavailable):
		writeProblem(w, r, loadUnavailable, "This build cannot load companies. Run the ingestion jobs instead.", nil)
	case err != nil && !isStockError(err):
		a.Logger.ErrorContext(r.Context(), "load failed", "ticker", r.PathValue("ticker"), "error", err)
		writeProblem(w, r, loadFailed, "A data source failed while loading the company. Try again later.", nil)
	default:
		a.respond(w, r, p, err)
	}
}

// isStockError reports whether err is one of the stocks errors that
// respond maps to a problem other than internal.
func isStockError(err error) bool {
	var missing *stocks.MissingInputError
	return errors.Is(err, stocks.ErrInvalidTicker) || errors.Is(err, stocks.ErrUnknownTicker) ||
		errors.Is(err, stocks.ErrNotLoaded) || errors.Is(err, engine.ErrInvalidInput) || errors.As(err, &missing)
}

// respond writes v, or the problem for the service error err.
func (a *api) respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	var missing *stocks.MissingInputError
	switch {
	case err == nil:
		writeJSON(a.Logger, w, r, v)
	case errors.Is(err, stocks.ErrInvalidTicker):
		writeProblem(w, r, invalidTicker, "A ticker is 1 to 10 letters, digits, '.' or '-'.", nil)
	case errors.Is(err, stocks.ErrUnknownTicker):
		writeProblem(w, r, unknownTicker, fmt.Sprintf("No company has the ticker %s.", strings.ToUpper(r.PathValue("ticker"))), nil)
	case errors.Is(err, stocks.ErrNotLoaded):
		loadable := true
		writeProblem(w, r, notLoaded, fmt.Sprintf("The data of %s is not loaded yet.", strings.ToUpper(r.PathValue("ticker"))),
			func(p *problem) { p.Loadable = &loadable })
	case errors.As(err, &missing):
		writeProblem(w, r, missingInput, fmt.Sprintf("The stock has no stored %s; send one.", missing.Input),
			func(p *problem) { p.Input = missing.Input })
	case errors.Is(err, engine.ErrInvalidInput):
		writeProblem(w, r, invalidLevers, leverDetail(err), nil)
	default:
		writeInternal(a.Logger, w, r, err)
	}
}

// leverDetail returns the engine's description of the bound that a lever
// broke, without the prefixes of the layers it passed.
func leverDetail(err error) string {
	msg := err.Error()
	if _, after, ok := strings.Cut(msg, engine.ErrInvalidInput.Error()+": "); ok {
		return after
	}
	return "A lever is out of bounds."
}
