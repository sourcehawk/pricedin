package pricestest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/pricestest"
)

var _ prices.Provider = (*pricestest.Provider)(nil)

func date(s string) domain.Date {
	d, err := domain.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func bar(symbol, day string, closePrice float64) domain.PriceBar {
	return domain.PriceBar{Symbol: symbol, Date: date(day), Close: closePrice, Source: "fake"}
}

func newFake() *pricestest.Provider {
	return &pricestest.Provider{
		Bars: []domain.PriceBar{
			bar("NVDA", "2024-06-10", 116),
			bar("AAPL", "2024-06-10", 190),
			bar("NVDA", "2024-06-07", 1150),
			bar("AAPL", "2024-06-07", 188),
			bar("NVDA", "2024-06-11", 117),
		},
		SplitEvents: []domain.Split{
			{Symbol: "NVDA", Date: date("2024-06-10"), From: 1, To: 10},
			{Symbol: "NVDA", Date: date("2021-07-20"), From: 1, To: 4},
			{Symbol: "AAPL", Date: date("2020-08-31"), From: 1, To: 4},
		},
		DividendEvents: []domain.Dividend{
			{Symbol: "KO", ExDate: date("2024-09-13"), CashAmount: 0.485},
			{Symbol: "KO", ExDate: date("2024-06-14"), CashAmount: 0.485},
			{Symbol: "AAPL", ExDate: date("2024-08-12"), CashAmount: 0.25},
		},
		TickerList: []prices.ProviderTicker{
			{Symbol: "NVDA", CIK: 1045810},
			{Symbol: "AAPL", CIK: 320193},
		},
	}
}

func TestNameDefaultsToFake(t *testing.T) {
	if got := (&pricestest.Provider{}).Name(); got != "fake" {
		t.Errorf("Name() = %q, want %q", got, "fake")
	}
	if got := (&pricestest.Provider{ProviderName: "massive"}).Name(); got != "massive" {
		t.Errorf("Name() = %q, want %q", got, "massive")
	}
}

func TestGroupedDailyReturnsEverySymbolOfTheDateBySymbol(t *testing.T) {
	got, err := newFake().GroupedDaily(t.Context(), date("2024-06-10"))
	if err != nil {
		t.Fatalf("GroupedDaily() error = %v", err)
	}
	want := []domain.PriceBar{bar("AAPL", "2024-06-10", 190), bar("NVDA", "2024-06-10", 116)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GroupedDaily() mismatch (-want +got):\n%s", diff)
	}
}

func TestGroupedDailyIsEmptyForADateWithoutBars(t *testing.T) {
	got, err := newFake().GroupedDaily(t.Context(), date("2024-06-08"))
	if err != nil || len(got) != 0 {
		t.Errorf("GroupedDaily(weekend) = %v, %v; want no bars and no error", got, err)
	}
}

func TestDailyBarsReturnsTheSymbolInTheRangeByDate(t *testing.T) {
	got, err := newFake().DailyBars(t.Context(), "NVDA", date("2024-06-07"), date("2024-06-10"))
	if err != nil {
		t.Fatalf("DailyBars() error = %v", err)
	}
	want := []domain.PriceBar{bar("NVDA", "2024-06-07", 1150), bar("NVDA", "2024-06-10", 116)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("DailyBars() mismatch (-want +got):\n%s", diff)
	}
}

func TestSplitsReturnsTheSymbolByDate(t *testing.T) {
	got, err := newFake().Splits(t.Context(), "NVDA")
	if err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	want := []domain.Split{
		{Symbol: "NVDA", Date: date("2021-07-20"), From: 1, To: 4},
		{Symbol: "NVDA", Date: date("2024-06-10"), From: 1, To: 10},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Splits() mismatch (-want +got):\n%s", diff)
	}
}

func TestDividendsReturnsTheSymbolByExDate(t *testing.T) {
	got, err := newFake().Dividends(t.Context(), "KO")
	if err != nil {
		t.Fatalf("Dividends() error = %v", err)
	}
	want := []domain.Dividend{
		{Symbol: "KO", ExDate: date("2024-06-14"), CashAmount: 0.485},
		{Symbol: "KO", ExDate: date("2024-09-13"), CashAmount: 0.485},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Dividends() mismatch (-want +got):\n%s", diff)
	}
}

func TestTickersReturnsEveryTickerBySymbol(t *testing.T) {
	got, err := newFake().Tickers(t.Context())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	want := []prices.ProviderTicker{{Symbol: "AAPL", CIK: 320193}, {Symbol: "NVDA", CIK: 1045810}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Tickers() mismatch (-want +got):\n%s", diff)
	}
}

func TestResultsAreCopies(t *testing.T) {
	fake := newFake()
	got, err := fake.Splits(t.Context(), "AAPL")
	if err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	got[0].To = 99
	again, err := fake.Splits(t.Context(), "AAPL")
	if err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	if again[0].To != 4 {
		t.Errorf("a change to a result changed the fake: To = %v, want 4", again[0].To)
	}
}

func TestDataBeforeAuthorizedFromIsNotAuthorized(t *testing.T) {
	fake := newFake()
	fake.AuthorizedFrom = date("2024-06-10")
	ctx := t.Context()

	if _, err := fake.GroupedDaily(ctx, date("2024-06-07")); !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("GroupedDaily(before) error = %v, want ErrNotAuthorized", err)
	}
	if _, err := fake.DailyBars(ctx, "NVDA", date("2024-06-01"), date("2024-06-07")); !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("DailyBars(entirely before) error = %v, want ErrNotAuthorized", err)
	}
	got, err := fake.DailyBars(ctx, "NVDA", date("2024-06-01"), date("2024-06-30"))
	if err != nil {
		t.Fatalf("DailyBars(partly before) error = %v", err)
	}
	want := []domain.PriceBar{bar("NVDA", "2024-06-10", 116), bar("NVDA", "2024-06-11", 117)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("DailyBars(partly before) mismatch, want the authorized part (-want +got):\n%s", diff)
	}
}

func TestErrFailsEveryCall(t *testing.T) {
	boom := errors.New("boom")
	fake := newFake()
	fake.Err = boom
	ctx := t.Context()
	errs := map[string]error{}
	_, errs["GroupedDaily"] = fake.GroupedDaily(ctx, date("2024-06-10"))
	_, errs["DailyBars"] = fake.DailyBars(ctx, "NVDA", date("2024-06-07"), date("2024-06-10"))
	_, errs["Splits"] = fake.Splits(ctx, "NVDA")
	_, errs["Dividends"] = fake.Dividends(ctx, "KO")
	_, errs["Tickers"] = fake.Tickers(ctx)
	for name, err := range errs {
		if !errors.Is(err, boom) {
			t.Errorf("%s() error = %v, want %v", name, err, boom)
		}
	}
}

func TestACanceledContextFailsEveryCall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fake := newFake()
	errs := map[string]error{}
	_, errs["GroupedDaily"] = fake.GroupedDaily(ctx, date("2024-06-10"))
	_, errs["DailyBars"] = fake.DailyBars(ctx, "NVDA", date("2024-06-07"), date("2024-06-10"))
	_, errs["Splits"] = fake.Splits(ctx, "NVDA")
	_, errs["Dividends"] = fake.Dividends(ctx, "KO")
	_, errs["Tickers"] = fake.Tickers(ctx)
	for name, err := range errs {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s() error = %v, want context.Canceled", name, err)
		}
	}
}

func TestDividendDatesAreCopies(t *testing.T) {
	pay := date("2024-07-01")
	fake := &pricestest.Provider{DividendEvents: []domain.Dividend{
		{Symbol: "KO", ExDate: date("2024-06-14"), PayDate: &pay, DeclarationDate: &pay},
	}}
	got, err := fake.Dividends(t.Context(), "KO")
	if err != nil {
		t.Fatalf("Dividends() error = %v", err)
	}
	*got[0].PayDate = date("2099-01-01")
	*got[0].DeclarationDate = date("2099-01-01")
	again, err := fake.Dividends(t.Context(), "KO")
	if err != nil {
		t.Fatalf("Dividends() error = %v", err)
	}
	if *again[0].PayDate != date("2024-07-01") || *again[0].DeclarationDate != date("2024-07-01") {
		t.Errorf("a change to a returned date changed the fake: pay %v, declared %v", again[0].PayDate, again[0].DeclarationDate)
	}
}

func TestBarsOfASessionThatIsNotOverAreLeftOut(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	fake := &pricestest.Provider{
		Bars: []domain.PriceBar{bar("AAPL", "2026-10-29", 1), bar("AAPL", "2026-10-30", 2)},
		Now:  func() time.Time { return time.Date(2026, time.October, 30, 15, 0, 0, 0, newYork) },
	}
	grouped, err := fake.GroupedDaily(t.Context(), date("2026-10-30"))
	if err != nil || len(grouped) != 0 {
		t.Errorf("GroupedDaily(today) = %v, %v; want no bars before 20:00 in New York", grouped, err)
	}
	daily, err := fake.DailyBars(t.Context(), "AAPL", date("2026-10-28"), date("2026-11-06"))
	if err != nil {
		t.Fatalf("DailyBars() error = %v", err)
	}
	if diff := cmp.Diff([]domain.PriceBar{bar("AAPL", "2026-10-29", 1)}, daily); diff != "" {
		t.Errorf("DailyBars() mismatch (-want +got):\n%s", diff)
	}
}
