package prices

import "github.com/sourcehawk/pricedin/server/internal/domain"

// StoredKind names the Provider method whose request a stored response
// page answered.
type StoredKind string

// The kinds of stored pages.
const (
	StoredTickers      StoredKind = "tickers"
	StoredSplits       StoredKind = "splits"
	StoredDividends    StoredKind = "dividends"
	StoredGroupedDaily StoredKind = "grouped-daily"
	StoredDailyBars    StoredKind = "daily-bars"
)

// StoredRequest is the request of one stored response page. Two pages of
// one request differ in Page only.
type StoredRequest struct {
	Kind StoredKind
	// Symbol is the provider's symbol of a splits, dividends or daily bars
	// request.
	Symbol string
	// Date is the day of a grouped daily request.
	Date domain.Date
	// From and To are the range of a daily bars request.
	From, To domain.Date
	// Page counts from 1.
	Page int
}

// StoredPage is the content of one stored response page. Only the field
// of the request's kind is set.
type StoredPage struct {
	// Last is true when no page follows this one.
	Last      bool
	Tickers   []ProviderTicker
	Splits    []domain.Split
	Dividends []domain.Dividend
	Bars      []domain.PriceBar
}

// StoredReader reads the response pages that a provider's client gave its
// recorder, so that a reprocess can load them again without a request.
type StoredReader interface {
	// Name is the provider's Name. The client records under the key prefix
	// "prices/<Name>/".
	Name() string
	// StoredRequest returns the request of the page that the client
	// recorded under base: the key before the recorder adds the retrieval
	// date and the hash. ok is false for a page that holds nothing that
	// PricedIn stores, such as split-adjusted bars. It returns an error for
	// a key that the client never makes.
	StoredRequest(base string) (req StoredRequest, ok bool, err error)
	// DecodeStored decodes a page of req into the items that the Provider
	// method of req.Kind returns, with the provider's symbols. A daily bars
	// page keeps only the bars up to req.To, as the client did. A page
	// whose status is an error gives the error of the Provider method.
	DecodeStored(req StoredRequest, body []byte) (StoredPage, error)
}
