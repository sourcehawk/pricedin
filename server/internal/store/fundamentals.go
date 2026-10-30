package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// QuarterlyValue is one derived value of one fiscal quarter of a company.
type QuarterlyValue struct {
	FiscalYear    int
	FiscalQuarter int // 1 to 4
	Metric        string
	Value         float64
	// Method is how the value was derived: "reported", "ytd_difference",
	// "fy_difference", "weighted_average_q4" or "derived". The database
	// rejects any other method.
	Method      string
	PeriodStart domain.Date
	PeriodEnd   domain.Date
	// Filed is the latest filing date of the inputs.
	Filed  domain.Date
	Inputs []SourceRef
}

// SourceRef names one reported fact that a derived value used.
type SourceRef struct {
	Accession string      `json:"accession"`
	Form      string      `json:"form"`
	Filed     domain.Date `json:"filed"`
	Concept   string      `json:"concept"`
}

var fundamentalsStage = stage{
	name: "stage_quarterly_fundamentals",
	ddl: `fiscal_year int, fiscal_quarter smallint, metric text, value numeric, method text,
		period_start date, period_end date, filed date, inputs jsonb`,
	columns: []string{"fiscal_year", "fiscal_quarter", "metric", "value", "method",
		"period_start", "period_end", "filed", "inputs"},
}

// ReplaceQuarterlyFundamentals replaces every derived value of the CIK with
// values, in one transaction: on an error the old values stay. The CIK must
// be a stored company, and each quarter and metric may appear once.
func (s *Store) ReplaceQuarterlyFundamentals(ctx context.Context, cik domain.CIK, values []QuarterlyValue) error {
	rows := make([][]any, len(values))
	for i, v := range values {
		inputs := v.Inputs
		if inputs == nil {
			inputs = []SourceRef{}
		}
		rows[i] = []any{v.FiscalYear, v.FiscalQuarter, v.Metric, v.Value, v.Method,
			pgDate(v.PeriodStart), pgDate(v.PeriodEnd), pgDate(v.Filed), inputs}
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM quarterly_fundamentals WHERE cik = $1`, int64(cik)); err != nil {
			return err
		}
		_, err := (&Store{db: tx}).copyInsert(ctx, fundamentalsStage, rows, `
			INSERT INTO quarterly_fundamentals (cik, fiscal_year, fiscal_quarter, metric, value, method,
			                                    period_start, period_end, filed, inputs)
			SELECT $1::bigint, fiscal_year, fiscal_quarter, metric, value, method,
			       period_start, period_end, filed, inputs
			FROM stage_quarterly_fundamentals`, int64(cik))
		return err
	})
	if err != nil {
		return fmt.Errorf("replace quarterly fundamentals of %s: %w", cik, err)
	}
	return nil
}

// ListQuarterlyFundamentals returns the derived values of the CIK, oldest
// quarter first, then by metric.
func (s *Store) ListQuarterlyFundamentals(ctx context.Context, cik domain.CIK) ([]QuarterlyValue, error) {
	rows, err := s.db.Query(ctx, `
		SELECT fiscal_year, fiscal_quarter, metric, value, method, period_start, period_end, filed, inputs
		FROM quarterly_fundamentals WHERE cik = $1
		ORDER BY fiscal_year, fiscal_quarter, metric`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list quarterly fundamentals of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuarterlyValue, error) {
		var v QuarterlyValue
		var start, end, filed pgtype.Date
		err := row.Scan(&v.FiscalYear, &v.FiscalQuarter, &v.Metric, &v.Value, &v.Method, &start, &end, &filed, &v.Inputs)
		v.PeriodStart, v.PeriodEnd, v.Filed = fromPgDate(start), fromPgDate(end), fromPgDate(filed)
		if len(v.Inputs) == 0 {
			v.Inputs = nil
		}
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("list quarterly fundamentals of %s: %w", cik, err)
	}
	return out, nil
}
