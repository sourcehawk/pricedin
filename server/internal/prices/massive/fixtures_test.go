package massive_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
)

// The synthetic fixtures that tools/price-fixtures writes. These tests
// check that they decode with this package and agree with the server's
// trading calendar.
const fixtureDir = "../../../../testdata/prices/massive"

func fixtureSymbols() []string {
	return []string{"AAPL", "GRMN", "KO", "NVDA"}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(fixtureDir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func fixtureDays() []domain.Date {
	return prices.TradingDays(date("2021-11-01"), date("2026-10-29"))
}

func TestGroupedFixturesExistForEveryTradingDayAndDecode(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(fixtureDir, "grouped"))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]domain.Date, 0, len(entries))
	for _, e := range entries {
		day := date(strings.TrimSuffix(e.Name(), ".json"))
		got = append(got, day)

		page, err := massive.ParseGroupedDaily(readFixture(t, "grouped/"+e.Name()))
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		symbols := make([]string, 0, len(page.Items))
		for _, b := range page.Items {
			symbols = append(symbols, b.Symbol)
			if b.Date != day {
				t.Errorf("%s: %s bar is dated %s", e.Name(), b.Symbol, b.Date)
			}
		}
		if !slices.Equal(fixtureSymbols(), symbols) {
			t.Errorf("%s: symbols %v, want %v", e.Name(), symbols, fixtureSymbols())
		}
	}
	if diff := cmp.Diff(fixtureDays(), got); diff != "" {
		t.Errorf("grouped fixture dates differ from prices.TradingDays (-want +got):\n%s", diff)
	}
}

func TestAggsFixturesCoverEveryTradingDay(t *testing.T) {
	for _, sym := range fixtureSymbols() {
		page, err := massive.ParseDailyBars(readFixture(t, "aggs/"+sym+".json"))
		if err != nil {
			t.Fatalf("%s: %v", sym, err)
		}
		dates := make([]domain.Date, 0, len(page.Items))
		for _, b := range page.Items {
			dates = append(dates, b.Date)
			if b.Symbol != sym {
				t.Fatalf("%s: bar of %s", sym, b.Symbol)
			}
		}
		if diff := cmp.Diff(fixtureDays(), dates); diff != "" {
			t.Errorf("%s: dates differ from prices.TradingDays (-want +got):\n%s", sym, diff)
		}
	}
}

func TestNVDAFixtureIsRawAndAdjustsSmoothly(t *testing.T) {
	bars, err := massive.ParseDailyBars(readFixture(t, "aggs/NVDA.json"))
	if err != nil {
		t.Fatal(err)
	}
	splits, err := massive.ParseSplits(readFixture(t, "splits/NVDA.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Split{{Symbol: "NVDA", Date: date("2024-06-10"), From: 1, To: 10, Source: "massive"}}
	if diff := cmp.Diff(want, splits.Items, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".Evidence" }, cmp.Ignore())); diff != "" {
		t.Errorf("NVDA splits mismatch (-want +got):\n%s", diff)
	}

	adjusted, err := prices.AdjustForSplits(bars.Items, splits.Items)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(adjusted); i++ {
		raw := bars.Items[i-1].Close / bars.Items[i].Close
		adj := adjusted[i-1].Close / adjusted[i].Close
		if bars.Items[i].Date == date("2024-06-10") {
			if raw < 5 {
				t.Errorf("raw close falls by %vx on the split day, want about 10x", raw)
			}
		}
		if math.Abs(math.Log(adj)) > math.Log(1.5) {
			t.Errorf("adjusted close moves %vx on %s", adj, adjusted[i].Bar.Date)
		}
	}
}

func TestDividendFixturesDecode(t *testing.T) {
	counts := map[string]int{}
	for _, sym := range fixtureSymbols() {
		page, err := massive.ParseDividends(readFixture(t, "dividends/"+sym+".json"))
		if err != nil {
			t.Fatalf("%s: %v", sym, err)
		}
		counts[sym] = len(page.Items)
		for _, d := range page.Items {
			if d.Symbol != sym || d.CashAmount <= 0 || d.Frequency != 4 || d.Type != "recurring" {
				t.Errorf("%s dividend %+v", sym, d)
			}
		}
	}
	if counts["KO"] < 20 || counts["GRMN"] < 20 || counts["AAPL"] != 0 || counts["NVDA"] != 0 {
		t.Errorf("dividend counts = %v, want about 20 for KO and GRMN and none for the others", counts)
	}
}

func TestTickerFixturesServeEveryPage(t *testing.T) {
	serveFile := func(w http.ResponseWriter, r *http.Request, path string) {
		body, err := os.ReadFile(filepath.Join(fixtureDir, filepath.FromSlash(path)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveJSON(string(body))(w, r)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/reference/tickers" {
			page := r.URL.Query().Get("cursor")
			if page == "" {
				page = "page-1"
			}
			serveFile(w, r, "tickers/"+page+".json")
			return
		}
		if sym, ok := strings.CutPrefix(r.URL.Path, "/v3/reference/tickers/"); ok {
			serveFile(w, r, "ticker/"+sym+".json")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := newClient(t, srv, nil)

	tickers, err := c.Tickers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ciks := map[string]domain.CIK{}
	for _, tk := range tickers {
		ciks[tk.Symbol] = tk.CIK
	}
	want := map[string]domain.CIK{"AAPL": 320193, "GRMN": 1121788, "KO": 21344, "NVDA": 1045810}
	if diff := cmp.Diff(want, ciks); diff != "" {
		t.Errorf("ticker CIKs mismatch (-want +got):\n%s", diff)
	}
	grmn, err := c.Ticker(t.Context(), "GRMN")
	if err != nil || grmn.CIK != 1121788 {
		t.Errorf("Ticker(GRMN) = %+v, %v; want CIK 1121788", grmn, err)
	}
}
