package domain

import (
	"fmt"
	"strings"
)

// Company is an SEC filer.
type Company struct {
	CIK            CIK
	Name           string
	SIC            string // four-digit Standard Industrial Classification code
	SICDescription string
	// FiscalYearEnd is the month and day the fiscal year ends, as "MMDD".
	FiscalYearEnd        string
	StateOfIncorporation string
}

// Ticker maps a trading symbol to a company for a span of dates.
type Ticker struct {
	Symbol    string
	CIK       CIK
	Exchange  string
	ValidFrom Date
	// ValidTo is nil while the symbol is current.
	ValidTo *Date
}

// Filing is one SEC submission, identified by its accession number.
type Filing struct {
	// Accession is the accession number with dashes, such as
	// "0001045810-24-000124".
	Accession       string
	CIK             CIK
	Form            string // such as "10-Q", "10-K" or "10-K/A"
	Filed           Date
	ReportDate      *Date
	PrimaryDocument string // file name of the main document; can be empty
}

// URL returns the link to the filing on EDGAR: the primary document, or the
// filing index page when PrimaryDocument is empty.
func (f Filing) URL() string {
	base := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/",
		int64(f.CIK), strings.ReplaceAll(f.Accession, "-", ""))
	if f.PrimaryDocument == "" {
		return base + f.Accession + "-index.htm"
	}
	return base + f.PrimaryDocument
}

// Fact is one value reported in an XBRL filing, as SEC company facts list it.
type Fact struct {
	CIK      CIK
	Taxonomy string // such as "us-gaap" or "dei"
	Concept  string // such as "EarningsPerShareDiluted"
	Unit     string // such as "USD", "USD/shares" or "shares"
	// PeriodStart is nil for an instant fact, such as a balance.
	PeriodStart  *Date
	PeriodEnd    Date
	Value        float64
	Form         string
	FiscalYear   int    // fiscal year of the filing, not of the period
	FiscalPeriod string // fiscal period of the filing: "Q1" to "Q3" or "FY"
	Frame        string // calendar frame such as "CY2024Q1"; often empty
	Filed        Date
	Accession    string
}

// PriceBar is one trading day of a symbol as the provider reports it. Prices
// are raw: not adjusted for splits or dividends.
type PriceBar struct {
	Symbol                 string
	Date                   Date
	Open, High, Low, Close float64
	// Volume is in shares. It is a float64 because the provider reports
	// fractional volume.
	Volume float64
	Source string // provider name
}

// Split is a stock split. After the split, each From shares became To
// shares: a 10-for-1 split has From 1 and To 10.
type Split struct {
	Symbol   string
	CIK      CIK // 0 when unknown
	Date     Date
	From, To float64
	Source   string // "sec" or the provider name
	// Evidence is the accession number or a note that supports the split.
	Evidence string
}

// Ratio returns the number of new shares for each old share (To / From).
// Divide per-share values from before the split by it.
func (s Split) Ratio() float64 {
	return s.To / s.From
}

// Dividend is a cash dividend per share, in Currency.
type Dividend struct {
	Symbol          string
	ExDate          Date
	PayDate         *Date
	DeclarationDate *Date
	CashAmount      float64
	Currency        string
	// Frequency is the number of payments a year, such as 4 for quarterly;
	// 0 for a one-time payment.
	Frequency int
	// Type is the provider's dividend type, such as "recurring" for a
	// regular cash dividend or "special" for a one-time one.
	Type       string
	Source     string
	ProviderID string
}
