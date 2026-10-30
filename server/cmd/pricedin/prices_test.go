package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

const verifyKey = "verify-key-0123456789"

// fakeMassive serves synthetic answers to the requests of prices verify.
// grmnCIK is the CIK that the ticker endpoint gives for GRMN.
// onlyAfterSplit leaves out the NVDA bar before the split.
func fakeMassive(t *testing.T, grmnCIK string, onlyAfterSplit bool) *httptest.Server {
	t.Helper()
	json := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	var mux http.ServeMux
	mux.HandleFunc("/stocks/v1/splits", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+verifyKey {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		json(w, `{"status":"OK","results":[
			{"id":"Eold","execution_date":"2021-07-20","split_from":1,"split_to":4,"ticker":"NVDA"},
			{"id":"Enew","execution_date":"2024-06-10","split_from":1,"split_to":10,"ticker":"NVDA"}]}`)
	})
	// Bars at midnight in New York of 2024-06-07 and 2024-06-10.
	mux.HandleFunc("/v2/aggs/ticker/NVDA/range/1/day/2024-06-05/2024-06-12", func(w http.ResponseWriter, r *http.Request) {
		if onlyAfterSplit {
			json(w, `{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":116,"v":1,"t":1717992000000}]}`)
			return
		}
		if r.URL.Query().Get("adjusted") == "true" {
			json(w, `{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":115,"v":1,"t":1717732800000},{"o":1,"h":1,"l":1,"c":116,"v":1,"t":1717992000000}]}`)
			return
		}
		json(w, `{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":1150,"v":1,"t":1717732800000},{"o":1,"h":1,"l":1,"c":116,"v":1,"t":1717992000000}]}`)
	})
	mux.HandleFunc("/stocks/v1/dividends", func(w http.ResponseWriter, _ *http.Request) {
		json(w, `{"status":"OK","results":[
			{"id":"E1","ticker":"KO","ex_dividend_date":"2023-11-30","pay_date":"2023-12-15","cash_amount":0.46,"currency":"USD","frequency":4,"distribution_type":"recurring"},
			{"id":"E2","ticker":"KO","ex_dividend_date":"2024-03-14","pay_date":"2024-04-01","cash_amount":0.485,"currency":"USD","frequency":4,"distribution_type":"recurring"},
			{"id":"E3","ticker":"KO","ex_dividend_date":"2024-06-14","pay_date":"2024-07-01","cash_amount":0.485,"currency":"USD","frequency":4,"distribution_type":"recurring"},
			{"id":"E4","ticker":"KO","ex_dividend_date":"2024-09-13","pay_date":"2024-10-01","cash_amount":0.485,"currency":"USD","frequency":4,"distribution_type":"recurring"},
			{"id":"E5","ticker":"KO","ex_dividend_date":"2024-11-29","pay_date":"2024-12-16","cash_amount":0.485,"currency":"USD","frequency":4,"distribution_type":"recurring"},
			{"id":"E6","ticker":"KO","ex_dividend_date":"2099-03-14","cash_amount":9.99,"currency":"USD","frequency":4,"distribution_type":"recurring"}]}`)
	})
	mux.HandleFunc("/v3/reference/tickers/GRMN", func(w http.ResponseWriter, _ *http.Request) {
		json(w, `{"status":"OK","results":{"ticker":"GRMN","name":"Garmin","cik":"`+grmnCIK+`"}}`)
	})
	srv := httptest.NewServer(&mux)
	t.Cleanup(srv.Close)
	return srv
}

func runVerify(t *testing.T, env map[string]string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(func(k string) string { return env[k] }, &out)
	cmd.SetArgs([]string{"prices", "verify"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestPricesVerifyRequiresTheAPIKey(t *testing.T) {
	_, err := runVerify(t, nil)
	if !errors.Is(err, config.ErrMissing) || !strings.Contains(err.Error(), "PRICEDIN_MASSIVE_API_KEY") {
		t.Errorf("prices verify error = %v, want a missing PRICEDIN_MASSIVE_API_KEY", err)
	}
}

func TestPricesVerifyReportsEveryCheck(t *testing.T) {
	srv := fakeMassive(t, "0001121788", false)
	out, err := runVerify(t, map[string]string{"PRICEDIN_MASSIVE_API_KEY": verifyKey, "PRICEDIN_MASSIVE_BASE_URL": srv.URL})
	if err != nil {
		t.Fatalf("prices verify error = %v\n%s", err, out)
	}
	for _, want := range []string{
		"2024-06-10  1 -> 10  (ratio 10)  id Enew",
		"2024-06-07  1150.0000  10  115.0000  115.0000  yes",
		"2024-06-10  116.0000  1  116.0000  116.0000  yes",
		"2024-03-14  2024-04-01  0.4850  USD  4  recurring",
		"2024-11-29  2024-12-16  0.4850  USD  4  recurring",
		"Massive: 0001121788",
		"SEC:  0001121788",
		"All checks passed.",
	} {
		if !strings.Contains(squeeze(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"2023-11-30", "2099-03-14", verifyKey} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q:\n%s", unwanted, out)
		}
	}
}

func TestPricesVerifyFailsWhenACheckFails(t *testing.T) {
	srv := fakeMassive(t, "0000000001", false)
	out, err := runVerify(t, map[string]string{"PRICEDIN_MASSIVE_API_KEY": verifyKey, "PRICEDIN_MASSIVE_BASE_URL": srv.URL})
	if err == nil {
		t.Fatalf("prices verify error = nil, want a failed CIK check\n%s", out)
	}
	if !strings.Contains(out, "does not match") {
		t.Errorf("output does not name the failed check:\n%s", out)
	}
}

// squeeze turns every run of two or more spaces into two spaces, so that the
// test does not depend on column widths.
func squeeze(s string) string {
	return regexp.MustCompile(` {2,}`).ReplaceAllString(s, "  ")
}

func TestPricesVerifyFailsWithoutBarsBeforeTheSplit(t *testing.T) {
	srv := fakeMassive(t, "0001121788", true)
	out, err := runVerify(t, map[string]string{"PRICEDIN_MASSIVE_API_KEY": verifyKey, "PRICEDIN_MASSIVE_BASE_URL": srv.URL})
	if err == nil {
		t.Fatalf("prices verify error = nil, want a failed split check\n%s", out)
	}
	if !strings.Contains(out, "before the split") {
		t.Errorf("output does not name the missing bars:\n%s", out)
	}
}
