package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// TickerSourceSEC is the source name of the SEC ticker map. When sources
// disagree on a symbol, the current mapping comes from this source.
const TickerSourceSEC = "sec"

// TickerChanges counts what SyncTickers wrote.
type TickerChanges struct {
	// Opened is the number of new current rows.
	Opened int64
	// Closed is the number of rows that stopped being current.
	Closed int64
	// Updated is the number of rows, opened on the same date, whose CIK or
	// exchange changed in place.
	Updated int64
}

// TickerRecord is one row of a source's dated symbol mapping.
type TickerRecord struct {
	domain.Ticker
	Source string
}

// TickerMatch is one search result.
type TickerMatch struct {
	Ticker domain.Ticker
	Name   string // company name
	// Loaded reports whether the company has derived quarterly
	// fundamentals.
	Loaded bool
}

var tickerStage = stage{
	name:    "stage_tickers",
	ddl:     `ord int, symbol text, cik bigint, exchange text`,
	columns: []string{"ord", "symbol", "cik", "exchange"},
}

// SyncTickers records current, the complete list of symbols that source
// lists on asOf. Only Symbol, CIK and Exchange of each ticker count. A new
// symbol opens a row valid from asOf. A symbol whose CIK or exchange changed
// closes its row with ValidTo asOf, the first day it no longer held, and
// opens a new one. A symbol that is not in current closes. Rows of other
// sources stay as they are. Each CIK must be a stored company. asOf must
// not be earlier than the last sync of the source, or SyncTickers returns an
// error and changes nothing. Syncs of one source run one after the other.
// Running the same sync again changes nothing.
func (s *Store) SyncTickers(ctx context.Context, source string, asOf domain.Date, current []domain.Ticker) (TickerChanges, error) {
	rows := make([][]any, len(current))
	for i, t := range current {
		rows[i] = []any{i, t.Symbol, int64(t.CIK), t.Exchange}
	}
	day := pgDate(asOf)
	var changes TickerChanges
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// The row lock makes a concurrent sync of the source wait, also on
		// its first sync.
		if _, err := tx.Exec(ctx,
			`INSERT INTO ticker_syncs (source, synced_on) VALUES ($1, $2) ON CONFLICT (source) DO NOTHING`,
			source, day); err != nil {
			return err
		}
		var last pgtype.Date
		if err := tx.QueryRow(ctx,
			`SELECT synced_on FROM ticker_syncs WHERE source = $1 FOR UPDATE`, source).Scan(&last); err != nil {
			return err
		}
		if fromPgDate(last).After(asOf) {
			return fmt.Errorf("%s synced on %s, after %s", source, fromPgDate(last), asOf)
		}
		if err := tickerStage.load(ctx, tx, rows); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			CREATE TEMP TABLE want_tickers AS
			SELECT DISTINCT ON (symbol) symbol, cik, exchange FROM stage_tickers ORDER BY symbol, ord DESC`); err != nil {
			return err
		}
		steps := []struct {
			sql   string
			count *int64
		}{
			// A row opened on asOf never held under its old values: change it
			// in place, or remove it when the symbol is gone.
			{`UPDATE tickers t SET cik = w.cik, exchange = w.exchange
				FROM want_tickers w
				WHERE t.source = $1 AND t.valid_to IS NULL AND t.valid_from = $2 AND t.symbol = w.symbol
				  AND (t.cik, t.exchange) IS DISTINCT FROM (w.cik, w.exchange)`, &changes.Updated},
			{`DELETE FROM tickers t
				WHERE t.source = $1 AND t.valid_to IS NULL AND t.valid_from = $2
				  AND NOT EXISTS (SELECT 1 FROM want_tickers w WHERE w.symbol = t.symbol)`, &changes.Closed},
			{`UPDATE tickers t SET valid_to = $2
				WHERE t.source = $1 AND t.valid_to IS NULL AND t.valid_from < $2
				  AND NOT EXISTS (SELECT 1 FROM want_tickers w
				                  WHERE w.symbol = t.symbol AND w.cik = t.cik AND w.exchange = t.exchange)`, &changes.Closed},
			{`INSERT INTO tickers (symbol, cik, exchange, valid_from, source)
				SELECT w.symbol, w.cik, w.exchange, $2, $1 FROM want_tickers w
				WHERE NOT EXISTS (SELECT 1 FROM tickers t
				                  WHERE t.source = $1 AND t.symbol = w.symbol AND t.valid_to IS NULL)`, &changes.Opened},
		}
		for _, step := range steps {
			tag, err := tx.Exec(ctx, step.sql, source, day)
			if err != nil {
				return err
			}
			*step.count += tag.RowsAffected()
		}
		if _, err := tx.Exec(ctx,
			`UPDATE ticker_syncs SET synced_on = $2, synced_at = now() WHERE source = $1`, source, day); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DROP TABLE want_tickers`); err != nil {
			return err
		}
		return tickerStage.drop(ctx, tx)
	})
	if err != nil {
		return TickerChanges{}, fmt.Errorf("sync tickers of %s: %w", source, err)
	}
	return changes, nil
}

// currentTickers selects one current row per symbol, preferring the SEC
// source.
const currentTickers = `
	SELECT DISTINCT ON (t.symbol) t.symbol, t.cik, t.exchange, t.valid_from
	FROM tickers t
	WHERE t.valid_to IS NULL
	ORDER BY t.symbol, (t.source = '` + TickerSourceSEC + `') DESC, t.source`

// GetCurrentTicker returns the current mapping of symbol, or ErrNotFound.
// The lookup upper-cases symbol, so store symbols in upper case. When more
// than one source maps the symbol, the SEC mapping wins.
func (s *Store) GetCurrentTicker(ctx context.Context, symbol string) (domain.Ticker, error) {
	var t domain.Ticker
	var cik int64
	var from pgtype.Date
	err := s.db.QueryRow(ctx, `
		SELECT symbol, cik, exchange, valid_from FROM tickers
		WHERE symbol = upper($1) AND valid_to IS NULL
		ORDER BY (source = '`+TickerSourceSEC+`') DESC, source
		LIMIT 1`, symbol).Scan(&t.Symbol, &cik, &t.Exchange, &from)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Ticker{}, notFound("get ticker %q", symbol)
	}
	if err != nil {
		return domain.Ticker{}, fmt.Errorf("get ticker %q: %w", symbol, err)
	}
	t.CIK = domain.CIK(cik)
	t.ValidFrom = fromPgDate(from)
	return t, nil
}

// ListTickers returns every mapping row of the CIK from every source, closed
// ones too, ordered by symbol, start date and source.
func (s *Store) ListTickers(ctx context.Context, cik domain.CIK) ([]TickerRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT symbol, cik, exchange, valid_from, valid_to, source FROM tickers
		WHERE cik = $1 ORDER BY symbol, valid_from, source`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list tickers of %s: %w", cik, err)
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
		return nil, fmt.Errorf("list tickers of %s: %w", cik, err)
	}
	return out, nil
}

// SearchTickers finds current symbols that start with query, or whose
// company name contains it, ignoring case. An exact symbol comes first, then
// other symbol matches, then name matches. It returns at most limit matches,
// and none for a blank query.
func (s *Store) SearchTickers(ctx context.Context, query string, limit int) ([]TickerMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return []TickerMatch{}, nil
	}
	// Backslash is the default LIKE escape character.
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.db.Query(ctx, `
		WITH cur AS (`+currentTickers+`)
		SELECT cur.symbol, cur.cik, cur.exchange, cur.valid_from, c.name,
		       EXISTS (SELECT 1 FROM quarterly_fundamentals q WHERE q.cik = cur.cik)
		FROM cur JOIN companies c ON c.cik = cur.cik
		WHERE cur.symbol LIKE upper($1) || '%' OR c.name ILIKE '%' || $1 || '%'
		ORDER BY cur.symbol = upper($2) DESC,
		         cur.symbol LIKE upper($1) || '%' DESC,
		         length(cur.symbol), cur.symbol
		LIMIT $3`, pattern, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search tickers %q: %w", query, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TickerMatch, error) {
		var m TickerMatch
		var cik int64
		var from pgtype.Date
		err := row.Scan(&m.Ticker.Symbol, &cik, &m.Ticker.Exchange, &from, &m.Name, &m.Loaded)
		m.Ticker.CIK, m.Ticker.ValidFrom = domain.CIK(cik), fromPgDate(from)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("search tickers %q: %w", query, err)
	}
	return out, nil
}
