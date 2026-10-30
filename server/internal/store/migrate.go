package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// AllowDropFactsSetting is a Postgres session setting. Rolling back the
// migration that creates facts fails while facts holds rows, unless the
// connection sets this setting to "on".
const AllowDropFactsSetting = "pricedin.allow_drop_facts"

//go:embed migrations/*.sql
var migrations embed.FS

// NewMigrator returns a goose provider that applies the embedded migrations
// to db, which must be a Postgres database. Goose records the applied
// versions in the table goose_db_version. Migrators on one database run one
// after the other.
func NewMigrator(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migrator: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("migrator: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrator: %w", err)
	}
	return p, nil
}
