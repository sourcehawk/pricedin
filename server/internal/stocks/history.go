package stocks

import (
	"context"
	"log/slog"
	"math"

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

// PriceFeed names where the daily closes of a History come from.
type PriceFeed struct {
	Provider string `json:"provider"`
	Symbol   string `json:"symbol"`
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
				st.log.Warn("value outside the engine's bounds left out",
					slog.String("quarter", q.Label), slog.String("metric", string(f.metric)), slog.Float64("value", x))
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
	for _, b := range st.bars {
		if b.Bar.Date.Before(from) {
			continue
		}
		p := engine.PricePoint{Date: b.Bar.Date, Close: b.Close}
		if _, v, ok := fundamentals.TTMKnownOn(st.series, fundamentals.EPSDiluted, b.Bar.Date); ok && math.Abs(v.V) <= maxPerShare {
			eps := boundedEPS(v.V)
			p.TTMEPS = &eps
		}
		out.History.Daily = append(out.History.Daily, p)
	}
	if b, ok := st.lastBar(); ok {
		out.Prices = &PriceFeed{Provider: b.Bar.Source, Symbol: b.Bar.Symbol}
	}
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
