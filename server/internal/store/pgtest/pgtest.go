// Package pgtest gives integration tests a real Postgres 18 database. A
// test package calls Main from its TestMain. Main starts one container for
// the package and migrates a template database once. Each test then gets a
// fresh database cloned from the template, so tests can run in parallel.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	// Registers the "pgx" driver for database/sql, which goose needs.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Image is the Postgres image of the db service in docker-compose.yml. A
// unit test fails when the two differ.
const Image = "postgres:18.6@sha256:979bccfde35445db618c65107d27d08c6c21339d8dc3ad4c830c6633947be0e4"

const (
	templateDB = "pricedin_template"
	adminDB    = "postgres"
	startLimit = 2 * time.Minute
)

// server is the container of the running test package. Main sets it before
// any test runs, and only Main writes it.
var server struct {
	container *postgres.PostgresContainer
	// adminURL connects to the maintenance database, from which tests
	// create and drop their databases.
	adminURL string
	// createMu serializes CREATE DATABASE, because Postgres refuses to copy
	// a template that another CREATE DATABASE is reading.
	createMu sync.Mutex
	seq      atomic.Int64
}

// Main starts the container, migrates the template database, runs the
// tests, stops the container, and exits with the result of m.Run.
func Main(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(m *testing.M) (code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), startLimit)
	defer cancel()

	c, err := postgres.Run(ctx, Image,
		postgres.WithDatabase(adminDB),
		postgres.WithUsername("pricedin"),
		postgres.WithPassword("pricedin"),
		postgres.BasicWaitStrategies(),
	)
	if c != nil {
		defer func() {
			if termErr := testcontainers.TerminateContainer(c); termErr != nil && err == nil {
				err = fmt.Errorf("stop container: %w", termErr)
			}
		}()
	}
	if err != nil {
		return 1, fmt.Errorf("start container: %w", err)
	}
	server.container = c
	server.adminURL, err = c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 1, fmt.Errorf("connection string: %w", err)
	}
	if err := createTemplate(ctx); err != nil {
		return 1, err
	}
	return m.Run(), nil
}

func createTemplate(ctx context.Context) error {
	if err := adminExec(ctx, "CREATE DATABASE "+templateDB); err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	db, err := sql.Open("pgx", urlFor(templateDB))
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	defer func() { _ = db.Close() }()
	migrator, err := store.NewMigrator(db)
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}
	return nil
}

// NewURL creates a database with the full schema and returns its
// connection URL. The database is dropped when the test ends.
func NewURL(tb testing.TB) string {
	tb.Helper()
	return newDatabase(tb, templateDB)
}

// NewEmptyURL creates a database with no schema and returns its connection
// URL, for tests of the migrations themselves. It is dropped when the test
// ends.
func NewEmptyURL(tb testing.TB) string {
	tb.Helper()
	return newDatabase(tb, "template0")
}

// NewPool returns a pool over a fresh database with the full schema. The
// pool is closed and the database dropped when the test ends.
func NewPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	pool, err := pgxpool.New(tb.Context(), NewURL(tb))
	if err != nil {
		tb.Fatalf("pgtest: open pool: %v", err)
	}
	// Registered after the drop, so it runs first: the drop needs the
	// pool's connections closed.
	tb.Cleanup(pool.Close)
	return pool
}

// NewStore returns a Store over a fresh database with the full schema.
func NewStore(tb testing.TB) *store.Store {
	tb.Helper()
	return store.New(NewPool(tb))
}

func newDatabase(tb testing.TB, template string) string {
	tb.Helper()
	if server.container == nil {
		tb.Fatal("pgtest: no container; call pgtest.Main from TestMain")
	}
	name := fmt.Sprintf("test_%d_%d", os.Getpid(), server.seq.Add(1))

	server.createMu.Lock()
	err := adminExec(tb.Context(), fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, template))
	server.createMu.Unlock()
	if err != nil {
		tb.Fatalf("pgtest: create database: %v", err)
	}
	tb.Cleanup(func() {
		// The test context is already canceled when cleanups run.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := adminExec(ctx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			tb.Errorf("pgtest: drop database: %v", err)
		}
	})
	return urlFor(name)
}

// Psql runs the psql script in the file at path on the database of dbURL,
// which NewURL or NewEmptyURL returned, as the user that owns the test
// databases. Each entry of vars is a psql variable (-v name=value). It
// returns what psql printed, and an error when psql exits with a status
// other than 0, as it does at the first failed statement.
func Psql(tb testing.TB, dbURL, path string, vars map[string]string) (string, error) {
	tb.Helper()
	if server.container == nil {
		tb.Fatal("pgtest: no container; call pgtest.Main from TestMain")
	}
	cfg, err := pgx.ParseConfig(dbURL)
	if err != nil {
		tb.Fatalf("pgtest: parse database URL: %v", err)
	}
	script, err := os.ReadFile(path) //nolint:gosec // G304: the test names a script of the repository.
	if err != nil {
		tb.Fatalf("pgtest: read %s: %v", path, err)
	}
	target := fmt.Sprintf("/tmp/pgtest-%d.sql", server.seq.Add(1))
	if err := server.container.CopyToContainer(tb.Context(), script, target, 0o644); err != nil {
		tb.Fatalf("pgtest: copy %s into the container: %v", path, err)
	}
	cmd := make([]string, 0, 10+2*len(vars))
	cmd = append(cmd, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", cfg.User, "-d", cfg.Database, "-f", target)
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		cmd = append(cmd, "-v", name+"="+vars[name])
	}
	code, out, err := server.container.Exec(tb.Context(), cmd, tcexec.Multiplexed())
	if err != nil {
		tb.Fatalf("pgtest: run psql: %v", err)
	}
	printed, err := io.ReadAll(out)
	if err != nil {
		tb.Fatalf("pgtest: read the output of psql: %v", err)
	}
	if code != 0 {
		return string(printed), fmt.Errorf("pgtest: psql exited with status %d", code)
	}
	return string(printed), nil
}

func adminExec(ctx context.Context, stmt string) error {
	conn, err := pgx.Connect(ctx, server.adminURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	_, err = conn.Exec(ctx, stmt)
	return err
}

// urlFor returns the URL of database name on the test server.
func urlFor(name string) string {
	cfg, err := pgx.ParseConfig(server.adminURL)
	if err != nil {
		// adminURL came from the container and parsed before.
		panic(fmt.Sprintf("pgtest: parse admin URL: %v", err))
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
}
