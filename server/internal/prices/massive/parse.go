package massive

import (
	"encoding/json"
	"fmt"
	"time"

	// The bar dates depend on the New York time zone, and a small server
	// image can lack the system time zone files.
	_ "time/tzdata"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

// source is the Source of every value that this package returns.
const source = "massive"

// Page is one decoded response page.
type Page[T any] struct {
	Items []T
	// NextURL is Massive's link to the next page, or empty on the last page.
	NextURL string
	// Notes names each value that the parse function could not read and
	// replaced, such as an invalid CIK that became 0.
	Notes []string
}

// The parse functions decode one stored or fetched response body. They need
// nothing but the body, so a reprocess can run them on the raw responses in
// the blob store. A body whose status is NOT_AUTHORIZED gives an error that
// wraps prices.ErrNotAuthorized; any other status than OK or DELAYED gives
// an *APIError.

// ParseGroupedDaily decodes a grouped daily response
// (/v2/aggs/grouped/locale/us/market/stocks/{date}).
func ParseGroupedDaily(body []byte) (Page[domain.PriceBar], error) {
	return parseAggs(body, true)
}

// ParseDailyBars decodes a per-ticker aggregates response
// (/v2/aggs/ticker/{ticker}/range/1/day/{from}/{to}).
func ParseDailyBars(body []byte) (Page[domain.PriceBar], error) {
	return parseAggs(body, false)
}

type envelope struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
	Error     string `json:"error"`
	NextURL   string `json:"next_url"`
}

func (e envelope) check() error {
	switch e.Status {
	case "OK", "DELAYED":
		return nil
	default:
		return &APIError{Status: e.Status, Message: e.message(), RequestID: e.RequestID}
	}
}

func (e envelope) message() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Error
}

type aggBar struct {
	Ticker string  `json:"T"`
	T      *int64  `json:"t"`
	Open   float64 `json:"o"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Close  float64 `json:"c"`
	Volume float64 `json:"v"`
}

func parseAggs(body []byte, grouped bool) (Page[domain.PriceBar], error) {
	var resp struct {
		envelope
		Ticker  string   `json:"ticker"`
		Results []aggBar `json:"results"`
	}
	if err := decode(body, &resp, &resp.envelope); err != nil {
		return Page[domain.PriceBar]{}, err
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		return Page[domain.PriceBar]{}, fmt.Errorf("massive: load time zone: %w", err)
	}
	bars := make([]domain.PriceBar, 0, len(resp.Results))
	for i, r := range resp.Results {
		symbol := resp.Ticker
		if grouped {
			symbol = r.Ticker
		}
		if symbol == "" {
			return Page[domain.PriceBar]{}, fmt.Errorf("massive: bar %d: no ticker", i)
		}
		if r.T == nil {
			return Page[domain.PriceBar]{}, fmt.Errorf("massive: bar %d of %s: no timestamp", i, symbol)
		}
		bars = append(bars, domain.PriceBar{
			Symbol: symbol,
			// t is a moment in the New York trading session, so its New
			// York calendar day is the session date in every season.
			Date:   domain.DateOf(time.UnixMilli(*r.T).In(newYork)),
			Open:   r.Open,
			High:   r.High,
			Low:    r.Low,
			Close:  r.Close,
			Volume: r.Volume,
			Source: source,
		})
	}
	return Page[domain.PriceBar]{Items: bars, NextURL: resp.NextURL}, nil
}

// ParseSplits decodes a splits response (/stocks/v1/splits). The Evidence
// of each split is Massive's identifier of the event.
func ParseSplits(body []byte) (Page[domain.Split], error) {
	var resp struct {
		envelope
		Results []struct {
			ID            string  `json:"id"`
			Ticker        string  `json:"ticker"`
			ExecutionDate string  `json:"execution_date"`
			SplitFrom     float64 `json:"split_from"`
			SplitTo       float64 `json:"split_to"`
		} `json:"results"`
	}
	if err := decode(body, &resp, &resp.envelope); err != nil {
		return Page[domain.Split]{}, err
	}
	splits := make([]domain.Split, 0, len(resp.Results))
	for _, r := range resp.Results {
		d, err := domain.ParseDate(r.ExecutionDate)
		if err != nil {
			return Page[domain.Split]{}, fmt.Errorf("massive: split %s of %s: %w", r.ID, r.Ticker, err)
		}
		splits = append(splits, domain.Split{
			Symbol:   r.Ticker,
			Date:     d,
			From:     r.SplitFrom,
			To:       r.SplitTo,
			Source:   source,
			Evidence: r.ID,
		})
	}
	return Page[domain.Split]{Items: splits, NextURL: resp.NextURL}, nil
}

// ParseDividends decodes a dividends response (/stocks/v1/dividends). The
// Type of each dividend is Massive's distribution type, such as "recurring"
// or "special". CashAmount is the amount as paid, not adjusted for later
// splits.
func ParseDividends(body []byte) (Page[domain.Dividend], error) {
	var resp struct {
		envelope
		Results []struct {
			ID               string  `json:"id"`
			Ticker           string  `json:"ticker"`
			ExDividendDate   string  `json:"ex_dividend_date"`
			PayDate          string  `json:"pay_date"`
			DeclarationDate  string  `json:"declaration_date"`
			CashAmount       float64 `json:"cash_amount"`
			Currency         string  `json:"currency"`
			Frequency        int     `json:"frequency"`
			DistributionType string  `json:"distribution_type"`
		} `json:"results"`
	}
	if err := decode(body, &resp, &resp.envelope); err != nil {
		return Page[domain.Dividend]{}, err
	}
	divs := make([]domain.Dividend, 0, len(resp.Results))
	for i := range resp.Results {
		r := &resp.Results[i]
		ex, err := domain.ParseDate(r.ExDividendDate)
		if err != nil {
			return Page[domain.Dividend]{}, fmt.Errorf("massive: dividend %s of %s: ex-dividend date: %w", r.ID, r.Ticker, err)
		}
		pay, err := optionalDate(r.PayDate)
		if err != nil {
			return Page[domain.Dividend]{}, fmt.Errorf("massive: dividend %s of %s: pay date: %w", r.ID, r.Ticker, err)
		}
		declared, err := optionalDate(r.DeclarationDate)
		if err != nil {
			return Page[domain.Dividend]{}, fmt.Errorf("massive: dividend %s of %s: declaration date: %w", r.ID, r.Ticker, err)
		}
		divs = append(divs, domain.Dividend{
			Symbol:          r.Ticker,
			ExDate:          ex,
			PayDate:         pay,
			DeclarationDate: declared,
			CashAmount:      r.CashAmount,
			Currency:        r.Currency,
			Frequency:       r.Frequency,
			Type:            r.DistributionType,
			Source:          source,
			ProviderID:      r.ID,
		})
	}
	return Page[domain.Dividend]{Items: divs, NextURL: resp.NextURL}, nil
}

type tickerJSON struct {
	Ticker          string `json:"ticker"`
	Name            string `json:"name"`
	PrimaryExchange string `json:"primary_exchange"`
	Type            string `json:"type"`
	CIK             string `json:"cik"`
	CompositeFIGI   string `json:"composite_figi"`
}

// toProvider returns the ticker, and a note when its CIK is not a valid CIK
// and became 0.
func (t tickerJSON) toProvider() (prices.ProviderTicker, string) {
	var cik domain.CIK
	var note string
	if t.CIK != "" {
		var err error
		if cik, err = domain.ParseCIK(t.CIK); err != nil {
			note = fmt.Sprintf("ticker %s: invalid CIK %q, using 0: %v", t.Ticker, t.CIK, err)
		}
	}
	return prices.ProviderTicker{
		Symbol:        t.Ticker,
		Name:          t.Name,
		CIK:           cik,
		Exchange:      t.PrimaryExchange,
		Type:          t.Type,
		CompositeFIGI: t.CompositeFIGI,
		Source:        source,
	}, note
}

// ParseTickers decodes a ticker list response (/v3/reference/tickers). A
// ticker whose CIK is not empty and not valid, such as "0000000000", gets
// CIK 0 and a note.
func ParseTickers(body []byte) (Page[prices.ProviderTicker], error) {
	var resp struct {
		envelope
		Results []tickerJSON `json:"results"`
	}
	if err := decode(body, &resp, &resp.envelope); err != nil {
		return Page[prices.ProviderTicker]{}, err
	}
	page := Page[prices.ProviderTicker]{Items: make([]prices.ProviderTicker, 0, len(resp.Results)), NextURL: resp.NextURL}
	for _, r := range resp.Results {
		t, note := r.toProvider()
		page.Items = append(page.Items, t)
		if note != "" {
			page.Notes = append(page.Notes, note)
		}
	}
	return page, nil
}

// ParseTickerOverview decodes a ticker overview response
// (/v3/reference/tickers/{ticker}) into a page with one ticker. Its CIK
// follows the rule of ParseTickers.
func ParseTickerOverview(body []byte) (Page[prices.ProviderTicker], error) {
	var resp struct {
		envelope
		Results tickerJSON `json:"results"`
	}
	if err := decode(body, &resp, &resp.envelope); err != nil {
		return Page[prices.ProviderTicker]{}, err
	}
	t, note := resp.Results.toProvider()
	page := Page[prices.ProviderTicker]{Items: []prices.ProviderTicker{t}}
	if note != "" {
		page.Notes = []string{note}
	}
	return page, nil
}

func decode(body []byte, v any, env *envelope) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("massive: decode response: %w", err)
	}
	return env.check()
}

func optionalDate(s string) (*domain.Date, error) {
	if s == "" {
		return nil, nil
	}
	d, err := domain.ParseDate(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// APIError is an error response from Massive. Its Unwrap gives
// prices.ErrNotAuthorized when Status is NOT_AUTHORIZED.
type APIError struct {
	// HTTPStatus is the HTTP status code, or 0 when the error came from a
	// body alone, as in a parse function.
	HTTPStatus int
	// Status is Massive's status, such as "ERROR", "NOT_FOUND" or
	// "NOT_AUTHORIZED". It can be empty.
	Status    string
	Message   string
	RequestID string
	// Path is the request path and query, with any API key removed.
	Path string
}

func (e *APIError) Error() string {
	msg := "massive:"
	if e.Path != "" {
		msg += " GET " + e.Path + ":"
	}
	if e.HTTPStatus != 0 {
		msg += fmt.Sprintf(" http %d", e.HTTPStatus)
	}
	if e.Status != "" {
		msg += " " + e.Status
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// Unwrap returns prices.ErrNotAuthorized for a NOT_AUTHORIZED status, or
// nil.
func (e *APIError) Unwrap() error {
	if e.Status == "NOT_AUTHORIZED" {
		return prices.ErrNotAuthorized
	}
	return nil
}

func decodeEnvelope(body []byte, env *envelope) error {
	return json.Unmarshal(body, env)
}
