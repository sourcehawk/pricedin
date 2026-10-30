package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// AdjustedBar is a daily bar divided by the splits that executed after its
// date and on or before the symbol's newest stored bar. Volume is multiplied
// by the same factor.
type AdjustedBar struct {
	domain.PriceBar
	RawClose float64
	// SplitFactor is the product of the ratios of those splits; 1 when none.
	SplitFactor float64
}

var priceBarStage = stage{
	name:    "stage_prices_daily",
	ddl:     `ord int, symbol text, date date, open numeric, high numeric, low numeric, close numeric, volume numeric, source text`,
	columns: []string{"ord", "symbol", "date", "open", "high", "low", "close", "volume", "source"},
}

// UpsertPriceBars stores raw daily bars. A bar for a new symbol and date is
// added. A stored bar changes only when the provider's values differ, which
// means the provider corrected it. It returns the number of bars added or
// changed, so loading the same batch twice writes nothing. When a batch holds
// a symbol and date twice, the later copy wins.
func (s *Store) UpsertPriceBars(ctx context.Context, bars []domain.PriceBar) (int64, error) {
	rows := make([][]any, len(bars))
	for i, b := range bars {
		rows[i] = []any{i, b.Symbol, pgDate(b.Date), b.Open, b.High, b.Low, b.Close, b.Volume, b.Source}
	}
	n, err := s.copyInsert(ctx, priceBarStage, rows, `
		INSERT INTO prices_daily (symbol, date, open, high, low, close, volume, source)
		SELECT DISTINCT ON (symbol, date) symbol, date, open, high, low, close, volume, source
		FROM stage_prices_daily
		ORDER BY symbol, date, ord DESC
		ON CONFLICT (symbol, date) DO UPDATE SET
			open = excluded.open, high = excluded.high, low = excluded.low, close = excluded.close,
			volume = excluded.volume, source = excluded.source, loaded_at = now()
		WHERE (prices_daily.open, prices_daily.high, prices_daily.low, prices_daily.close,
		       prices_daily.volume, prices_daily.source)
		      IS DISTINCT FROM
		      (excluded.open, excluded.high, excluded.low, excluded.close, excluded.volume, excluded.source)`)
	if err != nil {
		return 0, fmt.Errorf("upsert price bars: %w", err)
	}
	return n, nil
}

// ListPriceBars returns the raw bars of symbol from from to to, both
// included, oldest first.
func (s *Store) ListPriceBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, date, open, high, low, close, volume, source FROM prices_daily
		WHERE symbol = $1 AND date BETWEEN $2 AND $3 ORDER BY date`,
		symbol, pgDate(from), pgDate(to))
	if err != nil {
		return nil, fmt.Errorf("list price bars of %s: %w", symbol, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PriceBar, error) {
		var b domain.PriceBar
		var d pgtype.Date
		err := row.Scan(&b.Symbol, &d, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Source)
		b.Date = fromPgDate(d)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("list price bars of %s: %w", symbol, err)
	}
	return out, nil
}

// ListAdjustedPriceBars returns the split-adjusted bars of symbol from from
// to to, both included, oldest first. It reads the view prices_adjusted, so
// a split stored later changes the result without rewriting any bar. It
// applies the provider's splits only, not the SEC-confirmed splits of
// stock_splits, so its closes can disagree with per-share values that a
// split from SEC evidence alone adjusted.
func (s *Store) ListAdjustedPriceBars(ctx context.Context, symbol string, from, to domain.Date) ([]AdjustedBar, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, date, open, high, low, close, volume, source, raw_close, split_factor
		FROM prices_adjusted
		WHERE symbol = $1 AND date BETWEEN $2 AND $3 ORDER BY date`,
		symbol, pgDate(from), pgDate(to))
	if err != nil {
		return nil, fmt.Errorf("list adjusted price bars of %s: %w", symbol, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdjustedBar, error) {
		var b AdjustedBar
		var d pgtype.Date
		err := row.Scan(&b.Symbol, &d, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Source, &b.RawClose, &b.SplitFactor)
		b.Date = fromPgDate(d)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("list adjusted price bars of %s: %w", symbol, err)
	}
	return out, nil
}

// GetLatestPriceDate returns the date of the newest bar of symbol, or
// ErrNotFound when it has none.
func (s *Store) GetLatestPriceDate(ctx context.Context, symbol string) (domain.Date, error) {
	return s.latestDate(ctx, fmt.Sprintf("latest price date of %s", symbol),
		`SELECT max(date) FROM prices_daily WHERE symbol = $1`, symbol)
}

// GetEarliestPriceDate returns the date of the oldest bar of symbol, or
// ErrNotFound when it has none.
func (s *Store) GetEarliestPriceDate(ctx context.Context, symbol string) (domain.Date, error) {
	return s.latestDate(ctx, fmt.Sprintf("earliest price date of %s", symbol),
		`SELECT min(date) FROM prices_daily WHERE symbol = $1`, symbol)
}

// GetLatestPriceDateOverall returns the date of the newest bar of any
// symbol, or ErrNotFound when no bar is stored.
func (s *Store) GetLatestPriceDateOverall(ctx context.Context) (domain.Date, error) {
	return s.latestDate(ctx, "latest price date", `SELECT max(date) FROM prices_daily`)
}

func (s *Store) latestDate(ctx context.Context, what, sql string, args ...any) (domain.Date, error) {
	var d pgtype.Date
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&d); err != nil {
		return domain.Date{}, fmt.Errorf("%s: %w", what, err)
	}
	if !d.Valid {
		return domain.Date{}, notFound("%s", what)
	}
	return fromPgDate(d), nil
}

// ListMissingPriceDates returns the dates of tradingDays on which no symbol
// has a bar, oldest first and without repeats.
func (s *Store) ListMissingPriceDates(ctx context.Context, tradingDays []domain.Date) ([]domain.Date, error) {
	return s.missingDates(ctx, "list missing price dates", `
		SELECT DISTINCT d FROM unnest($1::date[]) AS d
		WHERE NOT EXISTS (SELECT 1 FROM prices_daily p WHERE p.date = d)
		ORDER BY d`, pgDates(tradingDays))
}

// ListMissingPriceDatesForSymbol returns the dates of tradingDays on which
// symbol has no bar, oldest first and without repeats.
func (s *Store) ListMissingPriceDatesForSymbol(ctx context.Context, symbol string, tradingDays []domain.Date) ([]domain.Date, error) {
	return s.missingDates(ctx, "list missing price dates of "+symbol, `
		SELECT DISTINCT d FROM unnest($1::date[]) AS d
		WHERE NOT EXISTS (SELECT 1 FROM prices_daily p WHERE p.symbol = $2 AND p.date = d)
		ORDER BY d`, pgDates(tradingDays), symbol)
}

func (s *Store) missingDates(ctx context.Context, what, sql string, args ...any) ([]domain.Date, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Date, error) {
		var d pgtype.Date
		err := row.Scan(&d)
		return fromPgDate(d), err
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

var priceSplitStage = stage{
	name:    "stage_price_splits",
	ddl:     `ord int, symbol text, execution_date date, split_from numeric, split_to numeric, provider_id text, source text`,
	columns: []string{"ord", "symbol", "execution_date", "split_from", "split_to", "provider_id", "source"},
}

// UpsertPriceSplits stores the provider's splits. A split is identified by
// its symbol and execution date (Split.Date); Split.Evidence holds the
// provider's identifier. A split whose identifier is stored under another
// date replaces that row, so a corrected date never counts twice. Loads of
// one source run one after the other. A stored split changes only when the
// provider's values differ. From and To must be positive. It returns the
// number of splits added or changed.
func (s *Store) UpsertPriceSplits(ctx context.Context, splits []domain.Split) (int64, error) {
	rows := make([][]any, len(splits))
	for i, sp := range splits {
		if sp.From <= 0 || sp.To <= 0 {
			return 0, fmt.Errorf("upsert price splits: split of %s on %s: from and to must be positive", sp.Symbol, sp.Date)
		}
		rows[i] = []any{i, sp.Symbol, pgDate(sp.Date), sp.From, sp.To, sp.Evidence, sp.Source}
	}
	if len(rows) == 0 {
		return 0, nil
	}
	var n int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := priceSplitStage.load(ctx, tx, rows); err != nil {
			return err
		}
		// Without the lock, two loads that move one provider event can both
		// delete nothing, and the second insert then fails on the unique
		// provider ID. Sources lock in one order, so loads cannot deadlock.
		if _, err := tx.Exec(ctx, `
			SELECT pg_advisory_xact_lock(hashtextextended('price_splits:' || source, 0))
			FROM (SELECT DISTINCT source FROM stage_price_splits ORDER BY source) s`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM stage_price_splits a USING stage_price_splits b
			WHERE a.provider_id <> '' AND a.source = b.source AND a.provider_id = b.provider_id AND a.ord < b.ord`); err != nil {
			return err
		}
		// A separate statement, so that the INSERT sees the freed
		// provider IDs.
		if _, err := tx.Exec(ctx, `
			DELETE FROM price_splits p USING stage_price_splits st
			WHERE st.provider_id <> '' AND p.source = st.source AND p.provider_id = st.provider_id
			  AND (p.symbol, p.execution_date) IS DISTINCT FROM (st.symbol, st.execution_date)`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO price_splits (symbol, execution_date, split_from, split_to, provider_id, source)
			SELECT DISTINCT ON (symbol, execution_date) symbol, execution_date, split_from, split_to, provider_id, source
			FROM stage_price_splits
			ORDER BY symbol, execution_date, ord DESC
			ON CONFLICT (symbol, execution_date) DO UPDATE SET
				split_from = excluded.split_from, split_to = excluded.split_to,
				provider_id = excluded.provider_id, source = excluded.source, loaded_at = now()
			WHERE (price_splits.split_from, price_splits.split_to, price_splits.provider_id, price_splits.source)
			      IS DISTINCT FROM
			      (excluded.split_from, excluded.split_to, excluded.provider_id, excluded.source)`)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return priceSplitStage.drop(ctx, tx)
	})
	if err != nil {
		return 0, fmt.Errorf("upsert price splits: %w", err)
	}
	return n, nil
}

// ListPriceSplits returns the provider's splits of symbol, oldest first,
// with the provider's identifier in Evidence.
func (s *Store) ListPriceSplits(ctx context.Context, symbol string) ([]domain.Split, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, execution_date, split_from, split_to, provider_id, source FROM price_splits
		WHERE symbol = $1 ORDER BY execution_date`, symbol)
	if err != nil {
		return nil, fmt.Errorf("list price splits of %s: %w", symbol, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Split, error) {
		var sp domain.Split
		var d pgtype.Date
		err := row.Scan(&sp.Symbol, &d, &sp.From, &sp.To, &sp.Evidence, &sp.Source)
		sp.Date = fromPgDate(d)
		return sp, err
	})
	if err != nil {
		return nil, fmt.Errorf("list price splits of %s: %w", symbol, err)
	}
	return out, nil
}

var dividendStage = stage{
	name: "stage_dividends",
	ddl: `ord int, provider_id text, symbol text, ex_dividend_date date, pay_date date, declaration_date date,
		cash_amount numeric, currency text, frequency int, dividend_type text, source text`,
	columns: []string{
		"ord", "provider_id", "symbol", "ex_dividend_date", "pay_date", "declaration_date",
		"cash_amount", "currency", "frequency", "dividend_type", "source",
	},
}

// UpsertDividends stores the provider's cash dividends, identified by
// ProviderID, which must not be empty. A stored dividend changes only when
// the provider's values differ. It returns the number of dividends added or
// changed.
func (s *Store) UpsertDividends(ctx context.Context, dividends []domain.Dividend) (int64, error) {
	rows := make([][]any, len(dividends))
	for i := range dividends {
		d := &dividends[i]
		if d.ProviderID == "" {
			return 0, fmt.Errorf("upsert dividends: dividend of %s on %s has no provider ID", d.Symbol, d.ExDate)
		}
		rows[i] = []any{
			i, d.ProviderID, d.Symbol, pgDate(d.ExDate), pgDatePtr(d.PayDate), pgDatePtr(d.DeclarationDate),
			d.CashAmount, d.Currency, d.Frequency, d.Type, d.Source,
		}
	}
	n, err := s.copyInsert(ctx, dividendStage, rows, `
		INSERT INTO dividends (provider_id, symbol, ex_dividend_date, pay_date, declaration_date,
		                       cash_amount, currency, frequency, dividend_type, source)
		SELECT DISTINCT ON (provider_id) provider_id, symbol, ex_dividend_date, pay_date, declaration_date,
		       cash_amount, currency, frequency, dividend_type, source
		FROM stage_dividends
		ORDER BY provider_id, ord DESC
		ON CONFLICT (provider_id) DO UPDATE SET
			symbol = excluded.symbol, ex_dividend_date = excluded.ex_dividend_date,
			pay_date = excluded.pay_date, declaration_date = excluded.declaration_date,
			cash_amount = excluded.cash_amount, currency = excluded.currency,
			frequency = excluded.frequency, dividend_type = excluded.dividend_type,
			source = excluded.source, loaded_at = now()
		WHERE (dividends.symbol, dividends.ex_dividend_date, dividends.pay_date, dividends.declaration_date,
		       dividends.cash_amount, dividends.currency, dividends.frequency, dividends.dividend_type,
		       dividends.source)
		      IS DISTINCT FROM
		      (excluded.symbol, excluded.ex_dividend_date, excluded.pay_date, excluded.declaration_date,
		       excluded.cash_amount, excluded.currency, excluded.frequency, excluded.dividend_type,
		       excluded.source)`)
	if err != nil {
		return 0, fmt.Errorf("upsert dividends: %w", err)
	}
	return n, nil
}

// ListDividends returns the dividends of symbol, oldest ex-dividend date
// first.
func (s *Store) ListDividends(ctx context.Context, symbol string) ([]domain.Dividend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT provider_id, symbol, ex_dividend_date, pay_date, declaration_date,
		       cash_amount, currency, frequency, dividend_type, source
		FROM dividends WHERE symbol = $1 ORDER BY ex_dividend_date, provider_id`, symbol)
	if err != nil {
		return nil, fmt.Errorf("list dividends of %s: %w", symbol, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Dividend, error) {
		var d domain.Dividend
		var ex, pay, decl pgtype.Date
		err := row.Scan(&d.ProviderID, &d.Symbol, &ex, &pay, &decl,
			&d.CashAmount, &d.Currency, &d.Frequency, &d.Type, &d.Source)
		d.ExDate, d.PayDate, d.DeclarationDate = fromPgDate(ex), fromPgDatePtr(pay), fromPgDatePtr(decl)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("list dividends of %s: %w", symbol, err)
	}
	return out, nil
}
