package stockstest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Seed writes the data of Fixture to the database of pool as the ingestion
// jobs would: the companies, the SEC tickers, the filings and facts, the
// quarters that fundamentals.Derive gives with the provider's splits, and
// the provider's bars, splits and dividends. The dividends have the load
// time Now. It returns the store over pool.
func Seed(tb testing.TB, pool *pgxpool.Pool) *store.Store {
	tb.Helper()
	ctx := context.Background()
	st := store.New(pool)
	m := Fixture(tb)
	must := func(_ int64, err error) {
		tb.Helper()
		if err != nil {
			tb.Fatalf("seed: %v", err)
		}
	}
	must(st.UpsertCompanies(ctx, m.Companies))
	tickers := make([]domain.Ticker, 0, len(m.Tickers))
	asOf := m.Tickers[0].ValidFrom
	for _, t := range m.Tickers {
		tickers = append(tickers, t.Ticker)
	}
	if _, err := st.SyncTickers(ctx, store.TickerSourceSEC, asOf, tickers); err != nil {
		tb.Fatalf("seed: %v", err)
	}
	must(st.InsertFilings(ctx, m.Filings))
	must(st.InsertFacts(ctx, m.Facts))
	must(st.UpsertPriceBars(ctx, m.Bars))
	must(st.UpsertPriceSplits(ctx, m.Splits))
	must(st.UpsertDividends(ctx, m.Dividends))
	symbols := make([]string, 0, len(m.DividendsLoadedAt))
	for sym := range m.DividendsLoadedAt {
		symbols = append(symbols, sym)
	}
	if _, err := pool.Exec(ctx, `UPDATE dividends SET loaded_at = $1 WHERE symbol = ANY($2)`, Now, symbols); err != nil {
		tb.Fatalf("seed: %v", err)
	}

	for _, t := range m.Tickers {
		if !m.Loaded[t.CIK] {
			continue
		}
		var facts []domain.Fact
		for i := range m.Facts {
			if m.Facts[i].CIK == t.CIK {
				facts = append(facts, m.Facts[i])
			}
		}
		var splits []domain.Split
		for _, sp := range m.Splits {
			if sp.Symbol == t.Symbol {
				splits = append(splits, sp)
			}
		}
		series, err := fundamentals.Derive(t.CIK, facts, fundamentals.Options{Splits: splits, ProviderChecked: true})
		if err != nil {
			tb.Fatalf("seed: derive %s: %v", t.Symbol, err)
		}
		if err := st.ReplaceQuarterlyFundamentals(ctx, t.CIK, quarterlyValues(series)); err != nil {
			tb.Fatalf("seed: %v", err)
		}
	}
	return st
}

// quarterlyValues converts a derived series to the rows of the store.
func quarterlyValues(s fundamentals.Series) []store.QuarterlyValue {
	var out []store.QuarterlyValue
	for _, q := range s.Quarters {
		for m, v := range q.Values {
			qv := store.QuarterlyValue{
				FiscalYear: q.FiscalYear, FiscalQuarter: q.FiscalQuarter, Metric: string(m), Value: v.V,
				Method: string(v.Method), PeriodStart: q.PeriodStart, PeriodEnd: q.PeriodEnd, Filed: v.Filed,
			}
			for i := range v.Inputs {
				in := &v.Inputs[i]
				qv.Inputs = append(qv.Inputs, store.SourceRef{Accession: in.Accession, Form: in.Form, Filed: in.Filed, Concept: in.Concept})
			}
			out = append(out, qv)
		}
	}
	return out
}
