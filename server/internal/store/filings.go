package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

var filingStage = stage{
	name:    "stage_filings",
	ddl:     `accession text, cik bigint, form text, filed date, report_date date, primary_document text`,
	columns: []string{"accession", "cik", "form", "filed", "report_date", "primary_document"},
}

// InsertFilings adds the filings that are not stored yet and returns how
// many it added. A stored filing never changes. Each CIK must be a stored
// company.
func (s *Store) InsertFilings(ctx context.Context, filings []domain.Filing) (int64, error) {
	rows := make([][]any, len(filings))
	for i, f := range filings {
		rows[i] = []any{f.Accession, int64(f.CIK), f.Form, pgDate(f.Filed), pgDatePtr(f.ReportDate), f.PrimaryDocument}
	}
	n, err := s.copyInsert(ctx, filingStage, rows, `
		INSERT INTO filings (accession, cik, form, filed, report_date, primary_document)
		SELECT accession, cik, form, filed, report_date, primary_document FROM stage_filings
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("insert filings: %w", err)
	}
	return n, nil
}

const filingColumns = `accession, cik, form, filed, report_date, primary_document`

func scanFiling(row pgx.Row) (domain.Filing, error) {
	var f domain.Filing
	var cik int64
	var filed, report pgtype.Date
	err := row.Scan(&f.Accession, &cik, &f.Form, &filed, &report, &f.PrimaryDocument)
	f.CIK, f.Filed, f.ReportDate = domain.CIK(cik), fromPgDate(filed), fromPgDatePtr(report)
	return f, err
}

// GetFiling returns the filing with the accession number, or ErrNotFound.
func (s *Store) GetFiling(ctx context.Context, accession string) (domain.Filing, error) {
	f, err := scanFiling(s.db.QueryRow(ctx, `SELECT `+filingColumns+` FROM filings WHERE accession = $1`, accession))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Filing{}, notFound("get filing %s", accession)
	}
	if err != nil {
		return domain.Filing{}, fmt.Errorf("get filing %s: %w", accession, err)
	}
	return f, nil
}

// ListFilings returns the filings of the CIK, oldest filing date first.
func (s *Store) ListFilings(ctx context.Context, cik domain.CIK) ([]domain.Filing, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+filingColumns+` FROM filings WHERE cik = $1 ORDER BY filed, accession`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list filings of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Filing, error) { return scanFiling(row) })
	if err != nil {
		return nil, fmt.Errorf("list filings of %s: %w", cik, err)
	}
	return out, nil
}
