package stocks

import (
	"context"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
)

// FundamentalValue is one derived value of a quarter.
type FundamentalValue struct {
	Value float64 `json:"value"`
	// Method is how the value was derived: reported, ytd_difference,
	// fy_difference, weighted_average_q4 or derived.
	Method   string `json:"method"`
	Estimate bool   `json:"estimate"`
	// Filed is the newest filing date among Inputs; FirstFiled the day the
	// filings first gave the quarter.
	Filed      domain.Date `json:"filed"`
	FirstFiled domain.Date `json:"firstFiled"`
	Inputs     []SECSource `json:"inputs"`
}

// FundamentalQuarter is one derived fiscal quarter.
type FundamentalQuarter struct {
	FiscalYear    int         `json:"fiscalYear"`
	FiscalQuarter int         `json:"fiscalQuarter"`
	Label         string      `json:"label"` // "Q<n> FY<yy>"
	PeriodStart   domain.Date `json:"periodStart"`
	PeriodEnd     domain.Date `json:"periodEnd"`
	// Values is keyed by metric name, such as eps_diluted; a metric the
	// filings do not give for the quarter is missing.
	Values map[string]FundamentalValue `json:"values"`
}

// SplitEvent is a confirmed stock split: From shares became To shares.
type SplitEvent struct {
	Date     domain.Date `json:"date"`
	From     float64     `json:"from"`
	To       float64     `json:"to"`
	Ratio    float64     `json:"ratio"`
	Source   string      `json:"source"`   // "sec" or the provider's name
	Evidence string      `json:"evidence"` // an accession number or the provider's identifier
}

// Conflict is a reconciliation problem of the derivation, for the data
// page. See fundamentals.Conflict for the kinds.
type Conflict struct {
	Kind        string       `json:"kind"`
	Concept     string       `json:"concept"`
	PeriodStart *domain.Date `json:"periodStart"`
	PeriodEnd   domain.Date  `json:"periodEnd"`
	// Original and Restated are the first and the latest filing of the
	// value, nil when the kind has none.
	Original *SECSource  `json:"original"`
	Restated *SECSource  `json:"restated"`
	Observed float64     `json:"observed"`
	Expected float64     `json:"expected"`
	Split    *SplitEvent `json:"split"`
}

// Fundamentals are a company's derived quarters with their sources.
type Fundamentals struct {
	Ticker    string               `json:"ticker"`
	CIK       string               `json:"cik"`
	Quarters  []FundamentalQuarter `json:"quarters"` // oldest first
	Splits    []SplitEvent         `json:"splits"`   // oldest first
	Conflicts []Conflict           `json:"conflicts"`
}

// Fundamentals returns the derived quarters of ticker. It gives
// ErrNotLoaded when the company's fundamentals are not loaded.
func (s *Service) Fundamentals(ctx context.Context, ticker string) (Fundamentals, error) {
	st, err := s.open(ctx, ticker)
	if err != nil {
		return Fundamentals{}, err
	}
	if err := s.withFundamentals(ctx, st); err != nil {
		return Fundamentals{}, err
	}
	cik := st.ticker.CIK
	out := Fundamentals{
		Ticker: st.ticker.Symbol, CIK: cik.String(),
		Quarters: make([]FundamentalQuarter, len(st.series.Quarters)), Splits: []SplitEvent{}, Conflicts: []Conflict{},
	}
	for i, q := range st.series.Quarters {
		fq := FundamentalQuarter{
			FiscalYear: q.FiscalYear, FiscalQuarter: q.FiscalQuarter, Label: q.Label,
			PeriodStart: q.PeriodStart, PeriodEnd: q.PeriodEnd, Values: make(map[string]FundamentalValue, len(q.Values)),
		}
		for m, v := range q.Values {
			fv := FundamentalValue{Value: v.V, Method: string(v.Method), Estimate: v.Estimate, Filed: v.Filed, FirstFiled: v.FirstFiled, Inputs: make([]SECSource, len(v.Inputs))}
			for j := range v.Inputs {
				fv.Inputs[j] = secSource(cik, v.Inputs[j], st.filings)
			}
			fq.Values[string(m)] = fv
		}
		out.Quarters[i] = fq
	}
	for _, sp := range st.series.Splits {
		out.Splits = append(out.Splits, splitEvent(sp))
	}
	for i := range st.series.Conflicts {
		c := &st.series.Conflicts[i]
		v := Conflict{
			Kind: string(c.Kind), Concept: c.Concept, PeriodStart: c.PeriodStart, PeriodEnd: c.PeriodEnd,
			Original: optionalSource(cik, c.Original, st.filings), Restated: optionalSource(cik, c.Restated, st.filings),
			Observed: c.Observed, Expected: c.Expected,
		}
		if c.Split != nil {
			sp := splitEvent(*c.Split)
			v.Split = &sp
		}
		out.Conflicts = append(out.Conflicts, v)
	}
	return out, nil
}

func splitEvent(sp domain.Split) SplitEvent {
	return SplitEvent{Date: sp.Date, From: sp.From, To: sp.To, Ratio: sp.Ratio(), Source: sp.Source, Evidence: sp.Evidence}
}

// optionalSource is nil for a zero SourceRef, which a conflict uses when it
// has no such filing.
func optionalSource(cik domain.CIK, ref fundamentals.SourceRef, filings map[string]domain.Filing) *SECSource {
	if ref.Accession == "" {
		return nil
	}
	s := secSource(cik, ref, filings)
	return &s
}
