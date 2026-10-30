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
	// Label is the fiscal quarter as the company names it, such as
	// "Q2 FY27".
	Label  string
	Metric string
	Value  float64
	// Method is how the value was derived: "reported", "ytd_difference",
	// "fy_difference", "weighted_average_q4" or "derived". The database
	// rejects any other method.
	Method string
	// Estimate is true when the value is not exact, such as a fourth
	// quarter share count from the weighted-average rule.
	Estimate    bool
	PeriodStart domain.Date
	PeriodEnd   domain.Date
	// Filed is the latest filing date of the inputs.
	Filed domain.Date
	// FirstFiled is the date on which the filings first gave the quarter;
	// zero when unknown.
	FirstFiled domain.Date
	Inputs     []SourceRef
}

// SourceRef names one reported fact that a derived value used.
type SourceRef struct {
	Accession string      `json:"accession"`
	Form      string      `json:"form"`
	Filed     domain.Date `json:"filed"`
	Concept   string      `json:"concept"`
	// PeriodStart is nil for an instant fact. The period is as filed.
	PeriodStart *domain.Date `json:"periodStart,omitempty"`
	PeriodEnd   domain.Date  `json:"periodEnd,omitzero"`
	// Value is the number as the filing reports it, before any split
	// adjustment or rescale.
	Value float64 `json:"value"`
	// SplitRatio is the product of the split ratios applied to Value, and
	// Scale the factor that corrected a share count on a wrong scale. Zero
	// means not recorded; read it as 1.
	SplitRatio float64 `json:"splitRatio,omitzero"`
	Scale      float64 `json:"scale,omitzero"`
}

var fundamentalsStage = stage{
	name: "stage_quarterly_fundamentals",
	ddl: `fiscal_year int, fiscal_quarter smallint, label text, metric text, value numeric, method text,
		estimate boolean, period_start date, period_end date, filed date, first_filed date, inputs jsonb`,
	columns: []string{
		"fiscal_year", "fiscal_quarter", "label", "metric", "value", "method",
		"estimate", "period_start", "period_end", "filed", "first_filed", "inputs",
	},
}

// ReplaceQuarterlyFundamentals replaces every derived value of the CIK with
// values, in one transaction: on an error the old values stay. Two
// replacements of one company run one after the other, so the result is
// always one complete set. The CIK must be a stored company, or it returns
// ErrNotFound, and each quarter and metric may appear once.
func (s *Store) ReplaceQuarterlyFundamentals(ctx context.Context, cik domain.CIK, values []QuarterlyValue) error {
	rows := make([][]any, len(values))
	for i := range values {
		v := &values[i]
		inputs := v.Inputs
		if inputs == nil {
			inputs = []SourceRef{}
		}
		rows[i] = []any{
			v.FiscalYear, v.FiscalQuarter, v.Label, v.Metric, v.Value, v.Method,
			v.Estimate, pgDate(v.PeriodStart), pgDate(v.PeriodEnd), pgDate(v.Filed), pgDate(v.FirstFiled), inputs,
		}
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// The DELETE alone locks nothing when the company has no rows yet.
		tag, err := tx.Exec(ctx, `SELECT FROM companies WHERE cik = $1 FOR NO KEY UPDATE`, int64(cik))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return notFound("company %s", cik)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM quarterly_fundamentals WHERE cik = $1`, int64(cik)); err != nil {
			return err
		}
		_, err = (&Store{db: tx}).copyInsert(ctx, fundamentalsStage, rows, `
			INSERT INTO quarterly_fundamentals (cik, fiscal_year, fiscal_quarter, label, metric, value, method,
			                                    estimate, period_start, period_end, filed, first_filed, inputs)
			SELECT $1::bigint, fiscal_year, fiscal_quarter, label, metric, value, method,
			       estimate, period_start, period_end, filed, first_filed, inputs
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
		SELECT fiscal_year, fiscal_quarter, label, metric, value, method, estimate,
		       period_start, period_end, filed, first_filed, inputs
		FROM quarterly_fundamentals WHERE cik = $1
		ORDER BY fiscal_year, fiscal_quarter, metric`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list quarterly fundamentals of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuarterlyValue, error) {
		var v QuarterlyValue
		var start, end, filed, firstFiled pgtype.Date
		err := row.Scan(&v.FiscalYear, &v.FiscalQuarter, &v.Label, &v.Metric, &v.Value, &v.Method, &v.Estimate,
			&start, &end, &filed, &firstFiled, &v.Inputs)
		v.PeriodStart, v.PeriodEnd, v.Filed = fromPgDate(start), fromPgDate(end), fromPgDate(filed)
		v.FirstFiled = fromPgDate(firstFiled)
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

// FundamentalConflict is one reconciliation problem that the derivation of
// a company's quarters found, for the data page.
type FundamentalConflict struct {
	// Kind is the derivation's conflict kind, such as "unconfirmed_split".
	Kind        string
	Concept     string
	PeriodStart *domain.Date
	// PeriodEnd is zero when the conflict has no period.
	PeriodEnd domain.Date
	// Original and Restated are the first and the latest filing of the
	// value; nil when the conflict has none.
	Original, Restated *SourceRef
	Observed, Expected float64
	// Split is the split that the conflict is about, or nil.
	Split *ConflictSplit
}

// ConflictSplit is the split of a FundamentalConflict.
type ConflictSplit struct {
	Date     domain.Date `json:"date"`
	From     float64     `json:"from"`
	To       float64     `json:"to"`
	Source   string      `json:"source"`
	Evidence string      `json:"evidence,omitempty"`
}

var conflictStage = stage{
	name: "stage_fundamental_conflicts",
	ddl: `ord int, kind text, concept text, period_start date, period_end date,
		original jsonb, restated jsonb, observed numeric, expected numeric, split jsonb`,
	columns: []string{
		"ord", "kind", "concept", "period_start", "period_end",
		"original", "restated", "observed", "expected", "split",
	},
}

// ReplaceFundamentalConflicts replaces every conflict of the CIK with
// conflicts, in one transaction, and keeps their order. The CIK must be a
// stored company when conflicts is not empty.
func (s *Store) ReplaceFundamentalConflicts(ctx context.Context, cik domain.CIK, conflicts []FundamentalConflict) error {
	rows := make([][]any, len(conflicts))
	for i := range conflicts {
		c := &conflicts[i]
		rows[i] = []any{
			i, c.Kind, c.Concept, pgDatePtr(c.PeriodStart), pgDate(c.PeriodEnd),
			c.Original, c.Restated, c.Observed, c.Expected, c.Split,
		}
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM fundamental_conflicts WHERE cik = $1`, int64(cik)); err != nil {
			return err
		}
		_, err := (&Store{db: tx}).copyInsert(ctx, conflictStage, rows, `
			INSERT INTO fundamental_conflicts (cik, ord, kind, concept, period_start, period_end,
			                                   original, restated, observed, expected, split)
			SELECT $1::bigint, ord, kind, concept, period_start, period_end,
			       original, restated, observed, expected, split
			FROM stage_fundamental_conflicts`, int64(cik))
		return err
	})
	if err != nil {
		return fmt.Errorf("replace fundamental conflicts of %s: %w", cik, err)
	}
	return nil
}

// ListFundamentalConflicts returns the conflicts of the CIK in the order
// they were stored.
func (s *Store) ListFundamentalConflicts(ctx context.Context, cik domain.CIK) ([]FundamentalConflict, error) {
	rows, err := s.db.Query(ctx, `
		SELECT kind, concept, period_start, period_end, original, restated, observed, expected, split
		FROM fundamental_conflicts WHERE cik = $1 ORDER BY ord`, int64(cik))
	if err != nil {
		return nil, fmt.Errorf("list fundamental conflicts of %s: %w", cik, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FundamentalConflict, error) {
		var c FundamentalConflict
		var start, end pgtype.Date
		err := row.Scan(&c.Kind, &c.Concept, &start, &end, &c.Original, &c.Restated, &c.Observed, &c.Expected, &c.Split)
		c.PeriodStart, c.PeriodEnd = fromPgDatePtr(start), fromPgDate(end)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("list fundamental conflicts of %s: %w", cik, err)
	}
	return out, nil
}
