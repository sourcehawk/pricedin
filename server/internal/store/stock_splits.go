package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

var stockSplitStage = stage{
	name:    "stage_stock_splits",
	ddl:     `cik bigint, effective_date date, ratio numeric, source text, evidence text`,
	columns: []string{"cik", "effective_date", "ratio", "source", "evidence"},
}

// InsertStockSplits adds the splits that SEC data confirms and that are not
// stored yet, and returns how many it added. A split is identified by its
// CIK, date and source; each CIK must be a stored company. Symbol is not
// stored. From and To must be positive.
func (s *Store) InsertStockSplits(ctx context.Context, splits []domain.Split) (int64, error) {
	rows := make([][]any, len(splits))
	for i, sp := range splits {
		if sp.From <= 0 || sp.To <= 0 {
			return 0, fmt.Errorf("insert stock splits: %s split of %s on %s: from and to must be positive", sp.Source, sp.CIK, sp.Date)
		}
		rows[i] = []any{int64(sp.CIK), pgDate(sp.Date), sp.Ratio(), sp.Source, sp.Evidence}
	}
	n, err := s.copyInsert(ctx, stockSplitStage, rows, `
		INSERT INTO stock_splits (cik, effective_date, ratio, source, evidence)
		SELECT cik, effective_date, ratio, source, evidence FROM stage_stock_splits
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("insert stock splits: %w", err)
	}
	return n, nil
}

// ListStockSplits returns the SEC-confirmed splits of the CIK, oldest first.
// The table keeps only the ratio, so each split comes back with From 1 and
// To equal to the ratio.
func (s *Store) ListStockSplits(ctx context.Context, cik domain.CIK) ([]domain.Split, error) {
	rows, err := s.db.Query(ctx, `
		SELECT cik, effective_date, ratio, source, evidence FROM stock_splits
		WHERE cik = $1 ORDER BY effective_date, source`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list stock splits of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Split, error) {
		sp := domain.Split{From: 1}
		var c int64
		var d pgtype.Date
		err := row.Scan(&c, &d, &sp.To, &sp.Source, &sp.Evidence)
		sp.CIK, sp.Date = domain.CIK(c), fromPgDate(d)
		return sp, err
	})
	if err != nil {
		return nil, fmt.Errorf("list stock splits of %s: %w", cik, err)
	}
	return out, nil
}
