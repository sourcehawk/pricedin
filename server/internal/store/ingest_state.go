package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// GetLastSucceededIngestRun returns the newest run of job that succeeded,
// or ErrNotFound.
func (s *Store) GetLastSucceededIngestRun(ctx context.Context, job string) (IngestRun, error) {
	r, err := scanIngestRun(s.db.QueryRow(ctx, `
		SELECT `+ingestRunColumns+` FROM ingest_runs
		WHERE job = $1 AND status = 'succeeded'
		ORDER BY started_at DESC, id DESC LIMIT 1`, job))
	if errors.Is(err, pgx.ErrNoRows) {
		return IngestRun{}, notFound("last succeeded run of %s", job)
	}
	if err != nil {
		return IngestRun{}, fmt.Errorf("last succeeded run of %s: %w", job, err)
	}
	return r, nil
}

// GetIngestCursor returns the position that job stored last, or
// ErrNotFound.
func (s *Store) GetIngestCursor(ctx context.Context, job string) (domain.Date, error) {
	var d pgtype.Date
	err := s.db.QueryRow(ctx, `SELECT position FROM ingest_cursors WHERE job = $1`, job).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Date{}, notFound("ingest cursor of %s", job)
	}
	if err != nil {
		return domain.Date{}, fmt.Errorf("ingest cursor of %s: %w", job, err)
	}
	return fromPgDate(d), nil
}

// SetIngestCursor stores the position of job. Storing the position that is
// already stored changes nothing.
func (s *Store) SetIngestCursor(ctx context.Context, job string, position domain.Date) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO ingest_cursors (job, position) VALUES ($1, $2)
		ON CONFLICT (job) DO UPDATE SET position = excluded.position, updated_at = now()
		WHERE ingest_cursors.position IS DISTINCT FROM excluded.position`, job, pgDate(position))
	if err != nil {
		return fmt.Errorf("set ingest cursor of %s: %w", job, err)
	}
	return nil
}

// MarkGroupedPricesLoaded records that the grouped daily bars of date were
// loaded, and that the provider returned bars of them. A date that is
// already marked keeps its first record.
func (s *Store) MarkGroupedPricesLoaded(ctx context.Context, date domain.Date, bars int) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO price_grouped_loads (date, bars) VALUES ($1, $2) ON CONFLICT (date) DO NOTHING`,
		pgDate(date), bars)
	if err != nil {
		return fmt.Errorf("mark grouped prices of %s loaded: %w", date, err)
	}
	return nil
}

// ListMissingGroupedPriceDates returns the dates of tradingDays whose
// grouped daily bars were not loaded yet, oldest first and without repeats.
// Bars that a load of one symbol stored do not count.
func (s *Store) ListMissingGroupedPriceDates(ctx context.Context, tradingDays []domain.Date) ([]domain.Date, error) {
	return s.missingDates(ctx, "list missing grouped price dates", `
		SELECT DISTINCT d FROM unnest($1::date[]) AS d
		WHERE NOT EXISTS (SELECT 1 FROM price_grouped_loads g WHERE g.date = d)
		ORDER BY d`, pgDates(tradingDays))
}

// GetLatestGroupedPriceDate returns the newest date whose grouped daily
// bars were loaded, or ErrNotFound.
func (s *Store) GetLatestGroupedPriceDate(ctx context.Context) (domain.Date, error) {
	return s.latestDate(ctx, "latest grouped price date", `SELECT max(date) FROM price_grouped_loads`)
}

// MarkPriceReferenceChecked records that the splits and dividends of the
// symbols were fetched from the provider. A symbol that is already marked
// keeps its first record. It returns the number of symbols marked now.
func (s *Store) MarkPriceReferenceChecked(ctx context.Context, symbols []string) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO price_reference_checks (symbol)
		SELECT DISTINCT unnest($1::text[]) ON CONFLICT (symbol) DO NOTHING`, symbols)
	if err != nil {
		return 0, fmt.Errorf("mark price reference checked: %w", err)
	}
	return tag.RowsAffected(), nil
}

// IsPriceReferenceChecked reports whether the splits and dividends of
// symbol were ever fetched from the provider.
func (s *Store) IsPriceReferenceChecked(ctx context.Context, symbol string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM price_reference_checks WHERE symbol = $1)`, symbol).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("price reference check of %s: %w", symbol, err)
	}
	return ok, nil
}

// ListCurrentTickers returns the current rows of source, ordered by symbol.
func (s *Store) ListCurrentTickers(ctx context.Context, source string) ([]domain.Ticker, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, cik, exchange, valid_from FROM tickers
		WHERE source = $1 AND valid_to IS NULL ORDER BY symbol`, source)
	if err != nil {
		return nil, fmt.Errorf("list current tickers of %s: %w", source, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Ticker, error) {
		var t domain.Ticker
		var cik int64
		var from pgtype.Date
		err := row.Scan(&t.Symbol, &cik, &t.Exchange, &from)
		t.CIK, t.ValidFrom = domain.CIK(cik), fromPgDate(from)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("list current tickers of %s: %w", source, err)
	}
	return out, nil
}

// ListCompanyCIKs returns the CIK of every stored company, in ascending
// order.
func (s *Store) ListCompanyCIKs(ctx context.Context) ([]domain.CIK, error) {
	rows, err := s.db.Query(ctx, `SELECT cik FROM companies ORDER BY cik`)
	if err != nil {
		return nil, fmt.Errorf("list company ciks: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CIK, error) {
		var c int64
		err := row.Scan(&c)
		return domain.CIK(c), err
	})
	if err != nil {
		return nil, fmt.Errorf("list company ciks: %w", err)
	}
	return out, nil
}

// MarkFundamentalsPending marks the companies whose derived quarters must
// be built again. A CIK that is not a stored company is skipped. Call it in
// the transaction that changes the inputs.
func (s *Store) MarkFundamentalsPending(ctx context.Context, ciks []domain.CIK) error {
	ids := make([]int64, len(ciks))
	for i, c := range ciks {
		ids[i] = int64(c)
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO fundamentals_pending (cik)
		SELECT DISTINCT c.cik FROM companies c WHERE c.cik = ANY($1::bigint[])
		ORDER BY c.cik
		ON CONFLICT (cik) DO UPDATE SET version = fundamentals_pending.version + 1, marked_at = now()`, ids)
	if err != nil {
		return fmt.Errorf("mark fundamentals pending: %w", err)
	}
	return nil
}

// MarkFundamentalsPendingForSymbols marks the companies that the symbols
// map to now in the SEC ticker map, as MarkFundamentalsPending does.
func (s *Store) MarkFundamentalsPendingForSymbols(ctx context.Context, symbols []string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO fundamentals_pending (cik)
		SELECT DISTINCT t.cik FROM tickers t
		WHERE t.source = '`+TickerSourceSEC+`' AND t.valid_to IS NULL AND t.symbol = ANY($1::text[])
		ORDER BY t.cik
		ON CONFLICT (cik) DO UPDATE SET version = fundamentals_pending.version + 1, marked_at = now()`, symbols)
	if err != nil {
		return fmt.Errorf("mark fundamentals pending: %w", err)
	}
	return nil
}

// GetFundamentalsPending returns the version of the CIK's mark, and false
// when the CIK is not marked.
func (s *Store) GetFundamentalsPending(ctx context.Context, cik domain.CIK) (int64, bool, error) {
	var v int64
	err := s.db.QueryRow(ctx, `SELECT version FROM fundamentals_pending WHERE cik = $1`, int64(cik)).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("fundamentals pending of %s: %w", cik, err)
	}
	return v, true, nil
}

// ClearFundamentalsPending removes the CIK's mark when its version is still
// version, and keeps a mark that a later change raised.
func (s *Store) ClearFundamentalsPending(ctx context.Context, cik domain.CIK, version int64) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM fundamentals_pending WHERE cik = $1 AND version = $2`, int64(cik), version); err != nil {
		return fmt.Errorf("clear fundamentals pending of %s: %w", cik, err)
	}
	return nil
}

// ListPendingFundamentals returns the marked CIKs in ascending order.
func (s *Store) ListPendingFundamentals(ctx context.Context) ([]domain.CIK, error) {
	rows, err := s.db.Query(ctx, `SELECT cik FROM fundamentals_pending ORDER BY cik`)
	if err != nil {
		return nil, fmt.Errorf("list pending fundamentals: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CIK, error) {
		var c int64
		err := row.Scan(&c)
		return domain.CIK(c), err
	})
	if err != nil {
		return nil, fmt.Errorf("list pending fundamentals: %w", err)
	}
	return out, nil
}

// LockCompanyFundamentals takes a transaction lock on the derived quarters
// of the CIK, so that two rebuilds of one company run one after the other.
// It must run in WithTx; the lock ends with the transaction.
func (s *Store) LockCompanyFundamentals(ctx context.Context, cik domain.CIK) error {
	if _, err := s.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fundamentals:' || $1::bigint::text, 0))`, int64(cik)); err != nil {
		return fmt.Errorf("lock fundamentals of %s: %w", cik, err)
	}
	return nil
}

// MarkFundamentalsPendingForSplitsReached marks pending the companies of
// the symbols whose bars, about to be stored, reach a provider split that
// no stored bar of the symbol reached yet: the split is dated after the
// symbol's newest stored bar and on or before the newest of bars. Call it
// in the transaction that stores bars, before it stores them.
func (s *Store) MarkFundamentalsPendingForSplitsReached(ctx context.Context, bars []domain.PriceBar) error {
	newest := map[string]domain.Date{}
	for _, b := range bars {
		if d, ok := newest[b.Symbol]; !ok || b.Date.After(d) {
			newest[b.Symbol] = b.Date
		}
	}
	if len(newest) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(newest))
	dates := make([]domain.Date, 0, len(newest))
	for sym, d := range newest {
		symbols = append(symbols, sym)
		dates = append(dates, d)
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO fundamentals_pending (cik)
		SELECT DISTINCT t.cik
		FROM unnest($1::text[], $2::date[]) AS b(symbol, upto)
		JOIN price_splits sp ON sp.symbol = b.symbol AND sp.execution_date <= b.upto
		  AND sp.execution_date > coalesce((SELECT max(p.date) FROM prices_daily p WHERE p.symbol = b.symbol), '-infinity')
		JOIN tickers t ON t.symbol = b.symbol AND t.source = '`+TickerSourceSEC+`' AND t.valid_to IS NULL
		ORDER BY t.cik
		ON CONFLICT (cik) DO UPDATE SET version = fundamentals_pending.version + 1, marked_at = now()`,
		symbols, pgDates(dates))
	if err != nil {
		return fmt.Errorf("mark fundamentals pending for splits: %w", err)
	}
	return nil
}

// GetEarliestGroupedPriceDate returns the oldest date whose grouped daily
// bars were loaded, or ErrNotFound.
func (s *Store) GetEarliestGroupedPriceDate(ctx context.Context) (domain.Date, error) {
	return s.latestDate(ctx, "earliest grouped price date", `SELECT min(date) FROM price_grouped_loads`)
}
