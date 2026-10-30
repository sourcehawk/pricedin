package massive

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

// Stored reads the response pages that a Client gave its recorder. It
// implements prices.StoredReader and needs no API key.
type Stored struct{}

// Name returns "massive", the Name of the Client.
func (Stored) Name() string { return source }

// StoredRequest implements prices.StoredReader for the keys of the Client.
func (Stored) StoredRequest(base string) (prices.StoredRequest, bool, error) {
	fail := func() (prices.StoredRequest, bool, error) {
		return prices.StoredRequest{}, false, fmt.Errorf("massive: %q is not a key of a recorded response", base)
	}
	rest, ok := strings.CutPrefix(base, "prices/"+source+"/")
	if !ok {
		return fail()
	}
	segs := strings.Split(rest, "/")
	page, ok := pageNumber(segs[len(segs)-1])
	if !ok {
		return fail()
	}
	params := segs[1 : len(segs)-1]
	req := prices.StoredRequest{Page: page}
	var err error
	switch {
	case segs[0] == "aggs-adjusted" || segs[0] == "ticker":
		return prices.StoredRequest{}, false, nil
	case segs[0] == "grouped" && len(params) == 1:
		req.Kind = prices.StoredGroupedDaily
		req.Date, err = domain.ParseDate(params[0])
	case segs[0] == "aggs" && len(params) == 2:
		req.Kind = prices.StoredDailyBars
		req.Symbol, err = url.PathUnescape(params[0])
		if err == nil {
			req.From, req.To, err = dateRange(params[1])
		}
	case (segs[0] == "splits" || segs[0] == "dividends") && len(params) == 1:
		req.Kind = prices.StoredSplits
		if segs[0] == "dividends" {
			req.Kind = prices.StoredDividends
		}
		req.Symbol, err = url.PathUnescape(params[0])
	case segs[0] == "tickers" && len(params) == 1 && params[0] == "stocks-active":
		req.Kind = prices.StoredTickers
	default:
		return fail()
	}
	if err != nil {
		return fail()
	}
	return req, true, nil
}

// pageNumber reads "page-<n>.json", n from 1.
func pageNumber(file string) (int, bool) {
	s, ok := strings.CutPrefix(file, "page-")
	if !ok {
		return 0, false
	}
	if s, ok = strings.CutSuffix(s, ".json"); !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1
}

// dateRange reads "<from>_<to>".
func dateRange(s string) (from, to domain.Date, err error) {
	a, b, ok := strings.Cut(s, "_")
	if !ok {
		return from, to, fmt.Errorf("massive: date range %q", s)
	}
	if from, err = domain.ParseDate(a); err != nil {
		return from, to, err
	}
	if to, err = domain.ParseDate(b); err != nil {
		return from, to, err
	}
	if from.After(to) {
		return from, to, fmt.Errorf("massive: date range %q ends before it starts", s)
	}
	return from, to, nil
}

// DecodeStored implements prices.StoredReader.
func (Stored) DecodeStored(req prices.StoredRequest, body []byte) (prices.StoredPage, error) {
	var out prices.StoredPage
	var next string
	switch req.Kind {
	case prices.StoredTickers:
		p, err := ParseTickers(body)
		if err != nil {
			return out, err
		}
		out.Tickers, next = p.Items, p.NextURL
	case prices.StoredSplits:
		p, err := ParseSplits(body)
		if err != nil {
			return out, err
		}
		out.Splits, next = p.Items, p.NextURL
	case prices.StoredDividends:
		p, err := ParseDividends(body)
		if err != nil {
			return out, err
		}
		out.Dividends, next = p.Items, p.NextURL
	case prices.StoredGroupedDaily:
		p, err := ParseGroupedDaily(body)
		if err != nil {
			return out, err
		}
		out.Bars, next = p.Items, p.NextURL
	case prices.StoredDailyBars:
		p, err := ParseDailyBars(body)
		if err != nil {
			return out, err
		}
		out.Bars, next = finishedOnly(p.Items, req.To), p.NextURL
	default:
		return out, fmt.Errorf("massive: decode stored page: unknown kind %q", req.Kind)
	}
	out.Last = next == ""
	return out, nil
}
