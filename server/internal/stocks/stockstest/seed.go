package stockstest

import (
	"context"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Seed writes the data of Fixture to st as the ingestion jobs would: the
// companies, the SEC tickers, the filings and facts, the quarters that
// fundamentals.Derive gives with the provider's splits, and the
// provider's bars, splits and dividends.
func Seed(tb testing.TB, st *store.Store) {
	tb.Helper()
	ctx := context.Background()
	m := Fixture(tb)
	must := func(_ int64, err error) {
		tb.Helper()
		if err != nil {
			tb.Fatalf("seed: %v", err)
		}
	}
	must(st.UpsertCompanies(ctx, m.Companies))
	var tickers []domain.Ticker
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

	for _, t := range m.Tickers {
		if !m.Loaded[t.CIK] {
			continue
		}
		var facts []domain.Fact
		for _, f := range m.Facts {
			if f.CIK == t.CIK {
				facts = append(facts, f)
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
			for _, in := range v.Inputs {
				qv.Inputs = append(qv.Inputs, store.SourceRef{Accession: in.Accession, Form: in.Form, Filed: in.Filed, Concept: in.Concept})
			}
			out = append(out, qv)
		}
	}
	return out
}
