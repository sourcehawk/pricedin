//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// TestTheOwnersFlowOverPostgres runs the API on a seeded database: log in,
// read NVIDIA across its split and Coca-Cola's dividend, evaluate, log out.
func TestTheOwnersFlowOverPostgres(t *testing.T) {
	st := pgtest.NewStore(t)
	stockstest.Seed(t, st)
	h := newHarnessOn(t, st, st)
	c := h.login(t)

	get := func(path string) response {
		t.Helper()
		res := h.do(t, withCookie(request(t, http.MethodGet, path, nil), c), true)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, body %.500s", path, res.StatusCode, res.body)
		}
		return res
	}
	get("/api/auth/session")
	get("/api/v1/search?q=coca")
	get("/api/v1/status")

	var nvda struct {
		History struct {
			Daily []struct {
				Date  string  `json:"date"`
				Close float64 `json:"close"`
			} `json:"daily"`
		} `json:"history"`
	}
	get("/api/v1/stocks/NVDA/history?range=3Y").json(t, &nvda)
	for _, d := range nvda.History.Daily {
		if d.Date == "2024-06-07" && d.Close != 73.13 {
			t.Errorf("NVDA close on 2024-06-07 = %v, want 73.13 after the 10-for-1 split", d.Close)
		}
	}
	get("/api/v1/stocks/NVDA/fundamentals")

	var ko struct {
		TTMDividend struct {
			Value float64 `json:"value"`
		} `json:"ttmDividend"`
	}
	get("/api/v1/stocks/KO").json(t, &ko)
	if ko.TTMDividend.Value < 2.0599 || ko.TTMDividend.Value > 2.0601 {
		t.Errorf("KO TTM dividend = %v, want 2.06", ko.TTMDividend.Value)
	}

	eval := withCookie(request(t, http.MethodPost, "/api/v1/stocks/KO/evaluation", map[string]any{"holdingYears": 5}), c)
	eval.Header.Set("Origin", webOrigin)
	if res := h.do(t, eval, true); res.StatusCode != http.StatusOK {
		t.Fatalf("evaluation: status %d, body %.500s", res.StatusCode, res.body)
	}

	out := withCookie(request(t, http.MethodPost, "/api/auth/logout", map[string]any{}), c)
	out.Header.Set("Origin", webOrigin)
	if res := h.do(t, out, true); res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: status %d", res.StatusCode)
	}
	h.do(t, withCookie(request(t, http.MethodGet, "/api/v1/status", nil), c), true).problem(t, http.StatusUnauthorized, "unauthenticated")
}

func TestAPITokenOverPostgres(t *testing.T) {
	st := pgtest.NewStore(t)
	stockstest.Seed(t, st)
	h := newHarnessOn(t, st, st)
	token := h.token(t)
	if res := h.do(t, withBearer(request(t, http.MethodGet, "/api/v1/stocks/NVDA", nil), token), true); res.StatusCode != http.StatusOK {
		t.Errorf("status %d, body %.300s", res.StatusCode, res.body)
	}
	h.do(t, request(t, http.MethodGet, "/api/v1/stocks/NVDA", nil), true).problem(t, http.StatusUnauthorized, "unauthenticated")
}
