package sec

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Submissions is the content of a company's submissions file
// (/submissions/CIK##########.json).
type Submissions struct {
	Company domain.Company
	// Filings are the filings that the file lists. ParseSubmissions returns
	// only the recent ones (filings.recent). Client.Submissions adds the
	// filings of the extra files.
	Filings []domain.Filing
	// Files are the names of the extra files that list older filings, such
	// as "CIK0001045810-submissions-001.json". They are on data.sec.gov
	// below /submissions/.
	Files []string
}

// ParseSubmissions decodes a company's submissions file: the company and the
// filings of filings.recent. An empty report date or primary document gives
// a nil ReportDate or an empty PrimaryDocument.
func ParseSubmissions(body []byte) (Submissions, error) {
	var doc struct {
		CIK                  *flexCIK `json:"cik"`
		Name                 string   `json:"name"`
		SIC                  string   `json:"sic"`
		SICDescription       string   `json:"sicDescription"`
		FiscalYearEnd        string   `json:"fiscalYearEnd"`
		StateOfIncorporation string   `json:"stateOfIncorporation"`
		Filings              struct {
			Recent filingColumns `json:"recent"`
			Files  []struct {
				Name string `json:"name"`
			} `json:"files"`
		} `json:"filings"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Submissions{}, fmt.Errorf("parse submissions: %w", err)
	}
	if doc.CIK == nil {
		return Submissions{}, errors.New("parse submissions: no cik")
	}
	cik := domain.CIK(*doc.CIK)
	filings, err := doc.Filings.Recent.filings(cik)
	if err != nil {
		return Submissions{}, fmt.Errorf("parse submissions: %w", err)
	}
	sub := Submissions{
		Company: domain.Company{
			CIK:                  cik,
			Name:                 doc.Name,
			SIC:                  doc.SIC,
			SICDescription:       doc.SICDescription,
			FiscalYearEnd:        doc.FiscalYearEnd,
			StateOfIncorporation: doc.StateOfIncorporation,
		},
		Filings: filings,
	}
	for _, f := range doc.Filings.Files {
		if f.Name == "" {
			return Submissions{}, errors.New("parse submissions: extra file without a name")
		}
		sub.Files = append(sub.Files, f.Name)
	}
	return sub, nil
}

// ParseSubmissionsFile decodes an extra submissions file of the company cik,
// such as CIK0001045810-submissions-001.json. The file does not name its
// company, so the caller passes it.
func ParseSubmissionsFile(cik domain.CIK, body []byte) ([]domain.Filing, error) {
	if cik <= 0 {
		return nil, fmt.Errorf("parse submissions file: invalid cik %d", int64(cik))
	}
	var cols filingColumns
	if err := json.Unmarshal(body, &cols); err != nil {
		return nil, fmt.Errorf("parse submissions file: %w", err)
	}
	filings, err := cols.filings(cik)
	if err != nil {
		return nil, fmt.Errorf("parse submissions file: %w", err)
	}
	return filings, nil
}

// filingColumns holds the filing list as SEC writes it: one array for each
// field, with one row for each filing at the same index in every array.
type filingColumns struct {
	AccessionNumber []string `json:"accessionNumber"`
	Form            []string `json:"form"`
	FilingDate      []string `json:"filingDate"`
	ReportDate      []string `json:"reportDate"`
	PrimaryDocument []string `json:"primaryDocument"`
}

func (c filingColumns) filings(cik domain.CIK) ([]domain.Filing, error) {
	n := len(c.AccessionNumber)
	for name, col := range map[string][]string{
		"form": c.Form, "filingDate": c.FilingDate, "reportDate": c.ReportDate, "primaryDocument": c.PrimaryDocument,
	} {
		if len(col) != n {
			return nil, fmt.Errorf("%s has %d rows, accessionNumber has %d", name, len(col), n)
		}
	}
	filings := make([]domain.Filing, 0, n)
	for i := range n {
		f := domain.Filing{
			Accession:       c.AccessionNumber[i],
			CIK:             cik,
			Form:            c.Form[i],
			PrimaryDocument: c.PrimaryDocument[i],
		}
		if f.Accession == "" {
			return nil, fmt.Errorf("row %d: no accession number", i)
		}
		var err error
		if f.Filed, err = domain.ParseDate(c.FilingDate[i]); err != nil {
			return nil, fmt.Errorf("%s: filing date: %w", f.Accession, err)
		}
		if c.ReportDate[i] != "" {
			report, err := domain.ParseDate(c.ReportDate[i])
			if err != nil {
				return nil, fmt.Errorf("%s: report date: %w", f.Accession, err)
			}
			f.ReportDate = &report
		}
		filings = append(filings, f)
	}
	return filings, nil
}
