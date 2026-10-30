package sec

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// ParseCompanyFacts decodes a company facts file
// (/api/xbrl/companyfacts/CIK##########.json) into one fact for each value
// of a kept concept (see Concepts). Each fact keeps the form, fiscal year,
// fiscal period, frame, filing date and accession number of the filing that
// reported it. A filing with no fiscal year or period gives 0 and "". The
// order is stable for the same input. It returns an error when the file has
// no CIK or when a kept value lacks its end date, value, form, filing date
// or accession number.
func ParseCompanyFacts(body []byte) ([]domain.Fact, error) {
	return parseCompanyFacts(body, 0)
}

type companyFactsDoc struct {
	CIK *flexCIK `json:"cik"`
	// A file has hundreds of concepts, and PricedIn decodes only the few
	// that it keeps.
	Facts map[string]map[string]json.RawMessage `json:"facts"`
}

type conceptDoc struct {
	Units map[string][]rawFact `json:"units"`
}

type rawFact struct {
	Start *string  `json:"start"`
	End   string   `json:"end"`
	Val   *float64 `json:"val"`
	Accn  string   `json:"accn"`
	FY    *int     `json:"fy"`
	FP    *string  `json:"fp"`
	Form  string   `json:"form"`
	Filed string   `json:"filed"`
	Frame string   `json:"frame"`
}

// parseCompanyFacts uses fallback when the file has no CIK, as the {} entries
// of the bulk archive do.
func parseCompanyFacts(body []byte, fallback domain.CIK) ([]domain.Fact, error) {
	var doc *companyFactsDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse company facts: %w", err)
	}
	if doc == nil {
		return nil, errors.New("parse company facts: empty document")
	}
	cik := fallback
	if doc.CIK != nil {
		if fallback != 0 && domain.CIK(*doc.CIK) != fallback {
			return nil, fmt.Errorf("parse company facts: cik %d, want %d", int64(*doc.CIK), int64(fallback))
		}
		cik = domain.CIK(*doc.CIK)
	}
	if cik == 0 {
		return nil, errors.New("parse company facts: no cik")
	}

	var facts []domain.Fact
	for _, concept := range Concepts() {
		raw, ok := doc.Facts[concept.Taxonomy()][string(concept)]
		if !ok {
			continue
		}
		var cd conceptDoc
		if err := json.Unmarshal(raw, &cd); err != nil {
			return nil, fmt.Errorf("parse company facts: %s: %w", concept, err)
		}
		units := make([]string, 0, len(cd.Units))
		for unit := range cd.Units {
			units = append(units, unit)
		}
		slices.Sort(units)
		for _, unit := range units {
			for _, rf := range cd.Units[unit] {
				f, err := rf.fact(cik, concept, unit)
				if err != nil {
					return nil, fmt.Errorf("parse company facts: %s %s: %w", concept, unit, err)
				}
				facts = append(facts, f)
			}
		}
	}
	return facts, nil
}

func (rf rawFact) fact(cik domain.CIK, concept Concept, unit string) (domain.Fact, error) {
	f := domain.Fact{
		CIK:       cik,
		Taxonomy:  concept.Taxonomy(),
		Concept:   string(concept),
		Unit:      unit,
		Form:      rf.Form,
		Frame:     rf.Frame,
		Accession: rf.Accn,
	}
	if rf.Val == nil {
		return f, errors.New("no value")
	}
	f.Value = *rf.Val
	if rf.Accn == "" {
		return f, errors.New("no accession number")
	}
	if rf.Form == "" {
		return f, fmt.Errorf("%s: no form", rf.Accn)
	}
	var err error
	if f.PeriodEnd, err = domain.ParseDate(rf.End); err != nil {
		return f, fmt.Errorf("%s: end: %w", rf.Accn, err)
	}
	if rf.Start != nil {
		start, err := domain.ParseDate(*rf.Start)
		if err != nil {
			return f, fmt.Errorf("%s: start: %w", rf.Accn, err)
		}
		f.PeriodStart = &start
	}
	if f.Filed, err = domain.ParseDate(rf.Filed); err != nil {
		return f, fmt.Errorf("%s: filed: %w", rf.Accn, err)
	}
	if rf.FY != nil {
		f.FiscalYear = *rf.FY
	}
	if rf.FP != nil {
		f.FiscalPeriod = *rf.FP
	}
	return f, nil
}

// flexCIK reads a CIK that SEC writes as a JSON number (company facts) or as
// a zero-padded string (submissions).
type flexCIK domain.CIK

func (c *flexCIK) UnmarshalJSON(data []byte) error {
	var s string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
	} else {
		s = string(data)
	}
	cik, err := domain.ParseCIK(s)
	if err != nil {
		return err
	}
	*c = flexCIK(cik)
	return nil
}
