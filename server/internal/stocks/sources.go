package stocks

import (
	"encoding/json"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
)

// SourceKind says where a number comes from. JSON carries it as "kind".
type SourceKind string

// The source kinds.
const (
	KindSEC      SourceKind = "sec"      // a value reported in an SEC filing
	KindPrice    SourceKind = "price"    // a close from the price provider
	KindDividend SourceKind = "dividend" // a cash dividend from the price provider
	KindDerived  SourceKind = "derived"  // computed from the inputs it lists
	KindUser     SourceKind = "user"     // typed by the owner
)

// Source is the provenance of one number: one of SECSource, PriceSource,
// DividendSource, DerivedSource or UserSource. It marshals with its kind.
type Source interface {
	Kind() SourceKind
}

// SECSource is one fact as an SEC filing reports it.
type SECSource struct {
	Form      string      `json:"form"`
	Accession string      `json:"accession"`
	Filed     domain.Date `json:"filed"`
	Concept   string      `json:"concept"`
	// PeriodStart is nil for an instant fact.
	PeriodStart *domain.Date `json:"periodStart"`
	PeriodEnd   domain.Date  `json:"periodEnd"`
	// Reported is the value as filed, before split adjustment.
	Reported float64 `json:"reported"`
	// SplitRatio divided a per-share value (or multiplied a share count);
	// 1 when no split applied.
	SplitRatio float64 `json:"splitRatio"`
	// Scale corrected a share count filed in thousands or millions; 1 when
	// none.
	Scale float64 `json:"scale"`
	URL   string  `json:"url"` // the filing on EDGAR
}

// PriceSource is one daily close of the price provider.
type PriceSource struct {
	Provider  string      `json:"provider"`
	Symbol    string      `json:"symbol"`
	CloseDate domain.Date `json:"closeDate"`
	RawClose  float64     `json:"rawClose"` // as the provider reports it
	// SplitFactor divided RawClose into the split-adjusted close; 1 when
	// no later split applies.
	SplitFactor float64 `json:"splitFactor"`
}

// DividendSource is one cash dividend of the price provider.
type DividendSource struct {
	Provider     string       `json:"provider"`
	Symbol       string       `json:"symbol"`
	ExDate       domain.Date  `json:"exDate"`
	PayDate      *domain.Date `json:"payDate"`
	Amount       float64      `json:"amount"`       // per share, as the provider reports it
	SplitRatio   float64      `json:"splitRatio"`   // divided Amount for the later splits; 1 when none
	DividendType string       `json:"dividendType"` // such as "recurring" or "special"
}

// DerivedSource is a number computed from other numbers.
type DerivedSource struct {
	// Method names the computation: a fundamentals method (ytd_difference,
	// fy_difference, weighted_average_q4, derived, ttm),
	// recurring_dividends_12m, or price_over_ttm_eps.
	Method string `json:"method"`
	// Estimate is set when the value is not exact.
	Estimate bool `json:"estimate"`
	// Label is the fiscal quarter that a TTM value ends with; empty
	// otherwise.
	Label  string   `json:"label,omitempty"`
	Inputs []Source `json:"inputs"`
}

// UserSource is a value that the owner typed.
type UserSource struct{}

// Kind returns KindSEC.
func (SECSource) Kind() SourceKind { return KindSEC }

// Kind returns KindPrice.
func (PriceSource) Kind() SourceKind { return KindPrice }

// Kind returns KindDividend.
func (DividendSource) Kind() SourceKind { return KindDividend }

// Kind returns KindDerived.
func (DerivedSource) Kind() SourceKind { return KindDerived }

// Kind returns KindUser.
func (UserSource) Kind() SourceKind { return KindUser }

// MarshalJSON adds "kind": "sec".
func (s SECSource) MarshalJSON() ([]byte, error) {
	type plain SECSource
	return withKindInline(KindSEC, plain(s))
}

// MarshalJSON adds "kind": "price".
func (s PriceSource) MarshalJSON() ([]byte, error) {
	type plain PriceSource
	return withKindInline(KindPrice, plain(s))
}

// MarshalJSON adds "kind": "dividend".
func (s DividendSource) MarshalJSON() ([]byte, error) {
	type plain DividendSource
	return withKindInline(KindDividend, plain(s))
}

// MarshalJSON adds "kind": "derived" and writes an empty input list as [].
func (s DerivedSource) MarshalJSON() ([]byte, error) {
	type plain DerivedSource
	if s.Inputs == nil {
		s.Inputs = []Source{}
	}
	return withKindInline(KindDerived, plain(s))
}

// MarshalJSON gives {"kind": "user"}.
func (UserSource) MarshalJSON() ([]byte, error) {
	return []byte(`{"kind":"user"}`), nil
}

// withKindInline marshals v, a struct, and puts "kind" first among its
// fields.
func withKindInline(kind SourceKind, v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	head, err := json.Marshal(kind)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(body)+len(head)+9)
	out = append(out, `{"kind":`...)
	out = append(out, head...)
	if len(body) > 2 {
		out = append(out, ',')
	}
	return append(out, body[1:]...), nil
}

// SourcedNumber is a number with its source and the date it is valid for.
type SourcedNumber struct {
	Value  float64     `json:"value"`
	Source Source      `json:"source"`
	AsOf   domain.Date `json:"asOf"`
	// AsOfLabel says how to label AsOf, such as "as of close"; empty when
	// the date needs no label.
	AsOfLabel string `json:"asOfLabel,omitempty"`
}

// secSource is the source of one input of a derived fundamental.
func secSource(cik domain.CIK, ref fundamentals.SourceRef, filings map[string]domain.Filing) SECSource {
	f, ok := filings[ref.Accession]
	if !ok {
		f = domain.Filing{Accession: ref.Accession, CIK: cik}
	}
	return SECSource{
		Form: ref.Form, Accession: ref.Accession, Filed: ref.Filed, Concept: ref.Concept,
		PeriodStart: ref.PeriodStart, PeriodEnd: ref.PeriodEnd, Reported: ref.Value,
		SplitRatio: orOne(ref.SplitRatio), Scale: orOne(ref.Scale), URL: f.URL(),
	}
}

// valueSource is the source of a derived fundamental: the filing for a
// value that one filing reports, else the derivation with its inputs.
func valueSource(cik domain.CIK, v fundamentals.Value, filings map[string]domain.Filing) Source {
	if v.Method == fundamentals.MethodReported && len(v.Inputs) == 1 {
		return secSource(cik, v.Inputs[0], filings)
	}
	d := DerivedSource{Method: string(v.Method), Estimate: v.Estimate, Inputs: make([]Source, len(v.Inputs))}
	for i, in := range v.Inputs {
		d.Inputs[i] = secSource(cik, in, filings)
	}
	return d
}

func orOne(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
