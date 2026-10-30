package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// stage is a temporary table that a batch write copies its rows into.
type stage struct {
	name    string
	ddl     string // column definitions, without the table name
	columns []string
}

// load creates the stage table in tx and copies rows into it with COPY.
// The caller drops it with drop before tx ends, so that a second batch in
// the same outer transaction can create it again.
func (st stage) load(ctx context.Context, tx pgx.Tx, rows [][]any) error {
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE "+st.name+" ("+st.ddl+")"); err != nil {
		return fmt.Errorf("create %s: %w", st.name, err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{st.name}, st.columns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy into %s: %w", st.name, err)
	}
	return nil
}

func (st stage) drop(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "DROP TABLE "+st.name); err != nil {
		return fmt.Errorf("drop %s: %w", st.name, err)
	}
	return nil
}

// copyInsert loads rows into st, runs insertSQL with args, which reads st,
// and returns the number of rows insertSQL wrote. An empty batch writes
// nothing.
func (s *Store) copyInsert(ctx context.Context, st stage, rows [][]any, insertSQL string, args ...any) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	var n int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := st.load(ctx, tx, rows); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, insertSQL, args...)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return st.drop(ctx, tx)
	})
	return n, err
}

func pgDate(d domain.Date) pgtype.Date {
	if d.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: d.Time(), Valid: true}
}

func pgDatePtr(d *domain.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return pgDate(*d)
}

func fromPgDate(d pgtype.Date) domain.Date {
	if !d.Valid {
		return domain.Date{}
	}
	return domain.DateOf(d.Time)
}

func fromPgDatePtr(d pgtype.Date) *domain.Date {
	if !d.Valid {
		return nil
	}
	v := domain.DateOf(d.Time)
	return &v
}

func fromPgTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func pgDates(dates []domain.Date) []pgtype.Date {
	out := make([]pgtype.Date, len(dates))
	for i, d := range dates {
		out[i] = pgDate(d)
	}
	return out
}
