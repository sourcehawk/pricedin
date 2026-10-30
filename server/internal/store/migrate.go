package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// NewMigrator returns a goose provider that applies the embedded migrations
// to db, which must be a Postgres database. Goose records the applied
// versions in the table goose_db_version.
func NewMigrator(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migrator: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return nil, fmt.Errorf("migrator: %w", err)
	}
	return p, nil
}
