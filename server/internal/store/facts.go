package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// FactQuery narrows ListFacts. The zero FactQuery returns every fact of the
// company.
type FactQuery struct {
	// AsOf, when set, keeps only the facts filed on or before it: what was
	// known on that day.
	AsOf domain.Date
	// Concepts, when not empty, keeps only these concepts.
	Concepts []string
}

var factStage = stage{
	name: "stage_facts",
	ddl: `cik bigint, taxonomy text, concept text, unit text, period_start date, period_end date,
		value numeric, form text, fiscal_year int, fiscal_period text, frame text, filed date, accession text`,
	columns: []string{
		"cik", "taxonomy", "concept", "unit", "period_start", "period_end",
		"value", "form", "fiscal_year", "fiscal_period", "frame", "filed", "accession",
	},
}

// InsertFacts adds the facts that are not stored yet and returns how many it
// added, so loading the same batch twice adds nothing. A fact's identity is
// its company, taxonomy, concept, unit, period, accession number and value.
// When copies of one identity differ in their calendar frame, the copy that
// has a frame is kept. Each CIK must be a stored company. Stored facts can
// never change or be removed.
func (s *Store) InsertFacts(ctx context.Context, facts []domain.Fact) (int64, error) {
	rows := make([][]any, len(facts))
	for i := range facts {
		f := &facts[i]
		rows[i] = []any{
			int64(f.CIK), f.Taxonomy, f.Concept, f.Unit, pgDatePtr(f.PeriodStart), pgDate(f.PeriodEnd),
			f.Value, f.Form, f.FiscalYear, f.FiscalPeriod, f.Frame, pgDate(f.Filed), f.Accession,
		}
	}
	n, err := s.copyInsert(ctx, factStage, rows, `
		INSERT INTO facts (cik, taxonomy, concept, unit, period_start, period_end, value,
		                   form, fiscal_year, fiscal_period, frame, filed, accession)
		SELECT DISTINCT ON (cik, taxonomy, concept, unit, coalesce(period_start, '-infinity'::date),
		                    period_end, accession, value)
		       cik, taxonomy, concept, unit, period_start, period_end, value,
		       form, fiscal_year, fiscal_period, frame, filed, accession
		FROM stage_facts
		ORDER BY cik, taxonomy, concept, unit, coalesce(period_start, '-infinity'::date),
		         period_end, accession, value, frame = ''
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("insert facts: %w", err)
	}
	return n, nil
}

// ListFacts returns the facts of the CIK that match q, ordered by concept,
// period end, period start, filing date and accession number.
func (s *Store) ListFacts(ctx context.Context, cik domain.CIK, q FactQuery) ([]domain.Fact, error) {
	var concepts []string
	if len(q.Concepts) > 0 {
		concepts = q.Concepts
	}
	rows, err := s.db.Query(ctx, `
		SELECT cik, taxonomy, concept, unit, period_start, period_end, value,
		       form, fiscal_year, fiscal_period, frame, filed, accession
		FROM facts
		WHERE cik = $1
		  AND ($2::date IS NULL OR filed <= $2)
		  AND ($3::text[] IS NULL OR concept = ANY ($3))
		ORDER BY concept, period_end, period_start NULLS FIRST, filed, accession, id`,
		int64(cik), pgDate(q.AsOf), concepts)
	if err != nil {
		return nil, fmt.Errorf("list facts of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Fact, error) {
		var f domain.Fact
		var c int64
		var start, end, filed pgtype.Date
		err := row.Scan(&c, &f.Taxonomy, &f.Concept, &f.Unit, &start, &end, &f.Value,
			&f.Form, &f.FiscalYear, &f.FiscalPeriod, &f.Frame, &filed, &f.Accession)
		f.CIK, f.PeriodStart, f.PeriodEnd, f.Filed = domain.CIK(c), fromPgDatePtr(start), fromPgDate(end), fromPgDate(filed)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("list facts of %s: %w", cik, err)
	}
	return out, nil
}

// GetLatestFiled returns the latest filing date among the stored facts of
// the CIK, or ErrNotFound when it has none.
func (s *Store) GetLatestFiled(ctx context.Context, cik domain.CIK) (domain.Date, error) {
	var d pgtype.Date
	if err := s.db.QueryRow(ctx, `SELECT max(filed) FROM facts WHERE cik = $1`, int64(cik)).Scan(&d); err != nil {
		return domain.Date{}, fmt.Errorf("latest filed of %s: %w", cik, err)
	}
	if !d.Valid {
		return domain.Date{}, notFound("latest filed of %s", cik)
	}
	return fromPgDate(d), nil
}

// ListLatestFiled returns, for each company with stored facts, the latest
// filing date among them.
func (s *Store) ListLatestFiled(ctx context.Context) (map[domain.CIK]domain.Date, error) {
	// One index lookup per company, not a scan of every fact.
	rows, err := s.db.Query(ctx, `
		SELECT c.cik, l.filed
		FROM companies c
		CROSS JOIN LATERAL (SELECT max(f.filed) AS filed FROM facts f WHERE f.cik = c.cik) l
		WHERE l.filed IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list latest filed: %w", err)
	}
	defer rows.Close()
	out := map[domain.CIK]domain.Date{}
	for rows.Next() {
		var cik int64
		var d pgtype.Date
		if err := rows.Scan(&cik, &d); err != nil {
			return nil, fmt.Errorf("list latest filed: %w", err)
		}
		out[domain.CIK(cik)] = fromPgDate(d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list latest filed: %w", err)
	}
	return out, nil
}
