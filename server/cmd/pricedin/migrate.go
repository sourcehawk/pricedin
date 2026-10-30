package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	// Registers the "pgx" driver for database/sql, which goose needs.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func newMigrateCmd(getenv func(string) string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back or list the database migrations",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("migrate: want a subcommand: up, down or status")
		},
	}

	up := &cobra.Command{
		Use:   "up",
		Short: "Apply every pending migration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withMigrator(cmd.Context(), getenv, func(p *goose.Provider, _ *sql.DB) error {
				results, err := p.Up(cmd.Context())
				printResults(cmd.OutOrStdout(), results)
				if len(results) == 0 && err == nil {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations")
				}
				return err
			})
		},
	}

	var to int64
	var force bool
	down := &cobra.Command{
		Use:   "down",
		Short: "Roll back the latest migration, or every migration after --to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withMigrator(cmd.Context(), getenv, func(p *goose.Provider, db *sql.DB) error {
				if !force {
					if err := refuseWhileFactsHasRows(cmd.Context(), db); err != nil {
						return err
					}
				}
				if !cmd.Flags().Changed("to") {
					result, err := p.Down(cmd.Context())
					if result != nil {
						printResults(cmd.OutOrStdout(), []*goose.MigrationResult{result})
					}
					return err
				}
				results, err := p.DownTo(cmd.Context(), to)
				printResults(cmd.OutOrStdout(), results)
				return err
			})
		},
	}
	down.Flags().Int64Var(&to, "to", 0, "roll back every migration newer than this version (0 rolls back all)")
	down.Flags().BoolVar(&force, "force", false, "roll back also when facts holds rows, which a rollback can delete")

	status := &cobra.Command{
		Use:   "status",
		Short: "List each migration as applied or pending",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withMigrator(cmd.Context(), getenv, func(p *goose.Provider, _ *sql.DB) error {
				statuses, err := p.Status(cmd.Context())
				if err != nil {
					return err
				}
				for _, s := range statuses {
					line := fmt.Sprintf("pending  %s", s.Source.Path)
					if s.State == goose.StateApplied {
						line = fmt.Sprintf("applied  %s  %s", s.Source.Path, s.AppliedAt.UTC().Format("2006-01-02 15:04:05Z"))
					}
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
				}
				return nil
			})
		},
	}

	cmd.AddCommand(up, down, status)
	return cmd
}

// withMigrator opens the configured database, runs fn with a migrator and
// the database, and closes the database.
func withMigrator(ctx context.Context, getenv func(string) string, fn func(*goose.Provider, *sql.DB) error) (err error) {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if err := cfg.Database.Require(); err != nil {
		return err
	}
	db, err := sql.Open("pgx", cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("migrate: open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	p, err := store.NewMigrator(db)
	if err != nil {
		return err
	}
	if err := fn(p, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// refuseWhileFactsHasRows returns an error when the table facts exists and
// holds rows. Facts are append-only, and a rollback can drop them.
func refuseWhileFactsHasRows(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.facts') IS NOT NULL`).Scan(&exists); err != nil {
		return fmt.Errorf("check facts: %w", err)
	}
	if !exists {
		return nil
	}
	var hasRows bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM facts)`).Scan(&hasRows); err != nil {
		return fmt.Errorf("check facts: %w", err)
	}
	if hasRows {
		return errors.New("facts holds rows, and a rollback can delete them; add --force to roll back anyway")
	}
	return nil
}

func printResults(w io.Writer, results []*goose.MigrationResult) {
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%s  %s  (%s)\n", r.Direction, r.Source.Path, r.Duration.Round(time.Millisecond))
	}
}
