// Package store reads and writes the PricedIn Postgres database. Methods are
// grouped by table, one file per concern. Financial values are numeric in
// the database and float64 in Go.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned, wrapped, by a Get method that finds no row and by
// an update of a row that does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned, wrapped, by an insert that breaks a unique rule,
// such as a second API token with the same name.
var ErrConflict = errors.New("store: conflict")

// db is the part of pgxpool.Pool and pgx.Tx that the store uses.
type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store is safe for concurrent use, except a Store that WithTx passes to its
// function: that one belongs to one transaction.
type Store struct {
	db db
}

// New returns a Store over pool. The caller owns the pool and closes it.
func New(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

// WithTx runs fn in one transaction. It commits when fn returns nil and
// rolls back when fn returns an error or panics. Inside fn, use only the
// Store that fn receives. A WithTx inside fn runs as a savepoint.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Store) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// inTx runs fn in a transaction, or in a savepoint when s is already in one.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.db, fn)
}

// isUniqueViolation reports whether err is a Postgres unique_violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// notFound wraps ErrNotFound with a short description of the missing row.
func notFound(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ErrNotFound)
}
