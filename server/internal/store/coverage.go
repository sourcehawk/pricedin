package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Coverage counts the stored companies by how much of their data is
// loaded.
type Coverage struct {
	Companies int64
	// WithFacts is the number of companies with at least one fact.
	WithFacts int64
	// WithFundamentals is the number of companies with derived quarterly
	// fundamentals, the ones that search reports as loaded.
	WithFundamentals int64
}

// GetCoverage returns the counts of the stored companies.
func (s *Store) GetCoverage(ctx context.Context) (Coverage, error) {
	var c Coverage
	err := s.db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM companies),
		       (SELECT count(*) FROM companies c WHERE EXISTS (SELECT 1 FROM facts f WHERE f.cik = c.cik)),
		       (SELECT count(DISTINCT cik) FROM quarterly_fundamentals)`).
		Scan(&c.Companies, &c.WithFacts, &c.WithFundamentals)
	if err != nil {
		return Coverage{}, fmt.Errorf("get coverage: %w", err)
	}
	return c, nil
}

// HasQuarterlyFundamentals reports whether the CIK has derived quarterly
// fundamentals: the Loaded flag of SearchTickers. An unknown CIK has none.
func (s *Store) HasQuarterlyFundamentals(ctx context.Context, cik domain.CIK) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM quarterly_fundamentals WHERE cik = $1)`, int64(cik)).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("has quarterly fundamentals of %s: %w", cik, err)
	}
	return ok, nil
}

// ListSymbolTickers returns every mapping row of symbol from every source,
// for every CIK and closed ones too, ordered by start date and source. The
// lookup upper-cases symbol. A symbol that moved from one company to
// another shows both, so a reader can tell which dates belong to which
// company.
func (s *Store) ListSymbolTickers(ctx context.Context, symbol string) ([]TickerRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, cik, exchange, valid_from, valid_to, source FROM tickers
		WHERE symbol = upper($1) ORDER BY valid_from, source`, symbol)
	if err != nil {
		return nil, fmt.Errorf("list tickers of symbol %q: %w", symbol, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TickerRecord, error) {
		var r TickerRecord
		var c int64
		var from, to pgtype.Date
		err := row.Scan(&r.Symbol, &c, &r.Exchange, &from, &to, &r.Source)
		r.CIK, r.ValidFrom, r.ValidTo = domain.CIK(c), fromPgDate(from), fromPgDatePtr(to)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("list tickers of symbol %q: %w", symbol, err)
	}
	return out, nil
}
