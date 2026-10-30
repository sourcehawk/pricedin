package stocks

import (
	"context"
	"log/slog"
	"math"
	"slices"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
)

// HistoryRange bounds the daily closes of a History.
type HistoryRange string

// The history ranges. Each range but RangeMax adds the twelve months and
// ten days before it: the 12-month change of the price chart's first day
// needs the last close at least 365 days earlier, which can fall before a
// weekend or a holiday.
const (
	Range6M  HistoryRange = "6M"
	Range1Y  HistoryRange = "1Y"
	Range3Y  HistoryRange = "3Y"
	RangeMax HistoryRange = "max" // every stored close
)

// ParseHistoryRange returns the range named s; an empty s is Range3Y.
func ParseHistoryRange(s string) (HistoryRange, bool) {
	switch r := HistoryRange(s); r {
	case "":
		return Range3Y, true
	case Range6M, Range1Y, Range3Y, RangeMax:
		return r, true
	default:
		return "", false
	}
}

// months returns the length of the range with the year before it, or 0
// for RangeMax.
func (r HistoryRange) months() int {
	switch r {
	case Range6M:
		return 18
	case Range1Y:
		return 24
	case Range3Y:
		return 48
	default:
		return 0
	}
}

// QuarterSources are the sources of the values of one quarter of a
// History. A nil source means that the value is null.
type QuarterSources struct {
	Label         string      `json:"label"`
	PeriodEnd     domain.Date `json:"periodEnd"`
	FiscalYear    int         `json:"fiscalYear"`
	FiscalQuarter int         `json:"fiscalQuarter"`
	// TTMEPSKnownOn is the first day on which the TTM EPS ending with this
	// quarter was known, nil when it has none. A daily close carries the
	// TTM EPS of the latest quarter known on its day.
	TTMEPSKnownOn   *domain.Date `json:"ttmEpsKnownOn"`
	EPS             Source       `json:"eps"`
	Close           *PriceSource `json:"close"`
	DilutedShares   Source       `json:"dilutedShares"`
	Revenue         Source       `json:"revenue"`
	OperatingIncome Source       `json:"operatingIncome"`
	OperatingCosts  Source       `json:"operatingCosts"`
	FreeCashFlow    Source       `json:"freeCashFlow"`
}

// PriceFeed names where the daily closes of a History come from, and the
// splits that adjust them. The raw close of a daily close, as the provider
// reports it, is the close times the ratio of every split dated after it.
type PriceFeed struct {
	// Provider is nil when the closes of the range come from more than one
	// provider.
	Provider *string `json:"provider"`
	Symbol   string  `json:"symbol"`
	// Splits are the splits after the first daily close, oldest first; empty
	// when no split adjusts a close of the range.
	Splits []PriceSplit `json:"splits"`
}

// PriceSplit is a split that adjusts the closes dated before it.
type PriceSplit struct {
	Date  domain.Date `json:"date"`
	Ratio float64     `json:"ratio"` // new shares for each old share
}

// History is the engine input of a stock with the sources of its values.
type History struct {
	Ticker string       `json:"ticker"`
	Range  HistoryRange `json:"range"`
	// History holds every derived quarter, oldest first, and the daily
	// closes of the range.
	History engine.History `json:"history"`
	// Quarters has one entry per quarter of History, in the same order.
	Quarters []QuarterSources `json:"quarters"`
	// Prices is nil when no close is stored.
	Prices    *PriceFeed `json:"prices"`
	Freshness Freshness  `json:"freshness"`
}

// History returns the engine input of ticker over the range. It gives
// ErrNotLoaded when the company's fundamentals are not loaded.
func (s *Service) History(ctx context.Context, ticker string, rng HistoryRange) (History, error) {
	st, err := s.open(ctx, ticker)
	if err != nil {
		return History{}, err
	}
	if err := s.withFundamentals(ctx, st); err != nil {
		return History{}, err
	}
	return st.history(rng), nil
}

func (st *stock) history(rng HistoryRange) History {
	out := History{
		Ticker: st.ticker.Symbol, Range: rng, Freshness: st.freshness(),
		History:  engine.History{Quarters: make([]engine.Quarter, len(st.series.Quarters)), Daily: []engine.PricePoint{}},
		Quarters: make([]QuarterSources, len(st.series.Quarters)),
	}
	cik := st.ticker.CIK
	for i, q := range st.series.Quarters {
		eq := engine.Quarter{Label: q.Label, PeriodEnd: q.PeriodEnd}
		qs := QuarterSources{Label: q.Label, PeriodEnd: q.PeriodEnd, FiscalYear: q.FiscalYear, FiscalQuarter: q.FiscalQuarter}
		for _, f := range []struct {
			metric fundamentals.Metric
			value  **float64
			source *Source
		}{
			{fundamentals.EPSDiluted, &eq.EPS, &qs.EPS},
			{fundamentals.DilutedShares, &eq.DilutedShares, &qs.DilutedShares},
			{fundamentals.Revenue, &eq.Revenue, &qs.Revenue},
			{fundamentals.OperatingIncome, &eq.OperatingIncome, &qs.OperatingIncome},
			{fundamentals.OperatingCosts, &eq.OperatingCosts, &qs.OperatingCosts},
			{fundamentals.FreeCashFlow, &eq.FreeCashFlow, &qs.FreeCashFlow},
		} {
			v, ok := q.Values[f.metric]
			if !ok {
				continue
			}
			x := v.V
			if f.metric == fundamentals.DilutedShares {
				x = math.Round(x) // whole shares
			}
			valid := amountOK(x)
			if f.metric == fundamentals.EPSDiluted {
				valid = perShareOK(x)
			}
			if !valid {
				st.warnOutOfBounds(string(f.metric), slog.String("quarter", q.Label), slog.Float64("value", x))
				continue
			}
			*f.value, *f.source = &x, valueSource(cik, v, st.filings)
		}
		if b, ok := st.closeOn(q.PeriodEnd); ok {
			c, src := b.Close, priceSource(b)
			eq.Close, qs.Close = &c, &src
		}
		if v, ok := fundamentals.TTM(st.series, fundamentals.EPSDiluted, i); ok {
			known := v.FirstFiled
			qs.TTMEPSKnownOn = &known
		}
		out.History.Quarters[i], out.Quarters[i] = eq, qs
	}

	from := st.dailyFrom(rng)
	known := st.ttmKnown()
	for _, b := range st.bars {
		if b.Bar.Date.Before(from) {
			continue
		}
		p := engine.PricePoint{Date: b.Bar.Date, Close: b.Close}
		if v, k, ok := known.on(b.Bar.Date); ok && math.Abs(v) <= maxPerShare {
			eps := boundedEPS(v)
			p.TTMEPS, p.TTMQuarter = &eps, &out.History.Quarters[k].Label
		}
		out.History.Daily = append(out.History.Daily, p)
	}
	if b, ok := st.lastBar(); ok {
		out.Prices = &PriceFeed{Provider: st.providerFrom(from), Symbol: b.Bar.Symbol, Splits: st.splitsAfter(out.History.Daily)}
	}
	return out
}

// providerFrom returns the provider of every bar from day on, or nil when
// they come from more than one.
func (st *stock) providerFrom(day domain.Date) *string {
	var p *string
	for _, b := range st.bars {
		switch {
		case b.Bar.Date.Before(day):
		case p == nil:
			p = &b.Bar.Source
		case *p != b.Bar.Source:
			return nil
		}
	}
	return p
}

// splitsAfter returns the splits in effect that are dated after the first
// of daily, oldest first.
func (st *stock) splitsAfter(daily []engine.PricePoint) []PriceSplit {
	out := []PriceSplit{}
	if len(daily) == 0 {
		return out
	}
	for _, sp := range st.splits {
		if daily[0].Date.Before(sp.Date) {
			out = append(out, PriceSplit{Date: sp.Date, Ratio: sp.Ratio()})
		}
	}
	slices.SortStableFunc(out, func(a, b PriceSplit) int { return a.Date.Time().Compare(b.Date.Time()) })
	return out
}

// lookBackSlack is the days a range starts before its 12-month look-back.
const lookBackSlack = 10

// dailyFrom returns the first day of the range, counted back from the
// newest close.
func (st *stock) dailyFrom(rng HistoryRange) domain.Date {
	last, ok := st.lastBar()
	if !ok || rng.months() == 0 {
		return earliest
	}
	return domain.DateOf(last.Bar.Date.Time().AddDate(0, -rng.months(), -lookBackSlack))
}

// ttmWalk answers fundamentals.TTMKnownOn for the EPS on days that never
// go back in time, in one pass over the bars.
type ttmWalk struct {
	ttm    []fundamentals.Value // by quarter index
	order  []int                // the quarters with a TTM, by the day it was known
	next   int                  // the first quarter of order not yet known
	latest int                  // the newest quarter known so far, or -1
}

func (st *stock) ttmKnown() *ttmWalk {
	w := &ttmWalk{ttm: make([]fundamentals.Value, len(st.series.Quarters)), latest: -1}
	for i := range st.series.Quarters {
		if v, ok := fundamentals.TTM(st.series, fundamentals.EPSDiluted, i); ok {
			w.ttm[i] = v
			w.order = append(w.order, i)
		}
	}
	slices.SortStableFunc(w.order, func(a, b int) int {
		return w.ttm[a].FirstFiled.Time().Compare(w.ttm[b].FirstFiled.Time())
	})
	return w
}

// on returns the TTM EPS known on day, as fundamentals.TTMKnownOn does, and
// the index of the newest quarter whose TTM was known by then. day must not
// be before the day of the previous call.
func (w *ttmWalk) on(day domain.Date) (float64, int, bool) {
	for w.next < len(w.order) && !w.ttm[w.order[w.next]].FirstFiled.After(day) {
		w.latest = max(w.latest, w.order[w.next])
		w.next++
	}
	if w.latest < 0 {
		return 0, 0, false
	}
	return w.ttm[w.latest].V, w.latest, true
}
