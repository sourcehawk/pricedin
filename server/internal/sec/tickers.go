package sec

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// TickerEntry is one row of the SEC ticker map: a ticker and the name of its
// company.
type TickerEntry struct {
	Ticker domain.Ticker
	Name   string
}

// ParseTickers decodes the SEC ticker map (/files/company_tickers_exchange.json).
// Each ticker gets ValidFrom = retrieved, the day the map was downloaded, and
// a nil ValidTo. A null exchange becomes "". One company can have several
// tickers, one for each share class.
func ParseTickers(body []byte, retrieved domain.Date) ([]TickerEntry, error) {
	var doc struct {
		Fields []string            `json:"fields"`
		Data   [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse tickers: %w", err)
	}
	col := map[string]int{}
	for i, f := range doc.Fields {
		col[f] = i
	}
	for _, f := range []string{"cik", "name", "ticker", "exchange"} {
		if _, ok := col[f]; !ok {
			return nil, fmt.Errorf("parse tickers: no %q field", f)
		}
	}
	entries := make([]TickerEntry, 0, len(doc.Data))
	for i, row := range doc.Data {
		e, err := tickerRow(row, col, retrieved)
		if err != nil {
			return nil, fmt.Errorf("parse tickers: row %d: %w", i, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func tickerRow(row []json.RawMessage, col map[string]int, retrieved domain.Date) (TickerEntry, error) {
	if len(row) != len(col) {
		return TickerEntry{}, fmt.Errorf("%d values, want %d", len(row), len(col))
	}
	var (
		cik                    flexCIK
		name, ticker, exchange *string
	)
	if err := json.Unmarshal(row[col["cik"]], &cik); err != nil {
		return TickerEntry{}, fmt.Errorf("cik: %w", err)
	}
	for field, dst := range map[string]**string{"name": &name, "ticker": &ticker, "exchange": &exchange} {
		if err := json.Unmarshal(row[col[field]], dst); err != nil {
			return TickerEntry{}, fmt.Errorf("%s: %w", field, err)
		}
	}
	if ticker == nil || *ticker == "" {
		return TickerEntry{}, errors.New("no ticker")
	}
	e := TickerEntry{Ticker: domain.Ticker{Symbol: *ticker, CIK: domain.CIK(cik), ValidFrom: retrieved}}
	if name != nil {
		e.Name = *name
	}
	if exchange != nil {
		e.Ticker.Exchange = *exchange
	}
	return e, nil
}
