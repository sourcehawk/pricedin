//go:build integration

package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

// rolesScript is the script that production runs as the owner to create
// the app role.
const rolesScript = "../../../deploy/sql/roles.sql"

// roleSeq keeps the role names of the tests apart: roles belong to the
// whole Postgres server, not to one test database.
var roleSeq atomic.Int64

func newRoleName() string {
	return fmt.Sprintf("pricedin_app_test_%d", roleSeq.Add(1))
}

// asUser returns dbURL with another user and password.
func asUser(t *testing.T, dbURL, user, password string) string {
	t.Helper()
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatalf("parse %q: %v", dbURL, err)
	}
	u.User = url.UserPassword(user, password)
	return u.String()
}

func ownerExec(t *testing.T, dbURL, stmt string) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(t.Context()) }()
	if _, err := conn.Exec(t.Context(), stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

func TestTheAppRoleRunsTheJobsButCannotChangeFacts(t *testing.T) {
	ownerURL := pgtest.NewEmptyURL(t)
	run(t, map[string]string{"PRICEDIN_DATABASE_URL": ownerURL}, "migrate", "up")
	role := newRoleName()
	if out, err := pgtest.Psql(t, ownerURL, rolesScript, map[string]string{"app_role": role}); err != nil {
		t.Fatalf("roles.sql: %v\n%s", err, out)
	}
	ownerExec(t, ownerURL, fmt.Sprintf("ALTER ROLE %s PASSWORD 'app-password'", role))
	appURL := asUser(t, ownerURL, role, "app-password")

	env := fixtureEnv(t)
	env["PRICEDIN_DATABASE_URL"] = appURL
	for _, job := range [][]string{
		{"sec-tickers"}, {"sec-bulk"}, {"price-reference"}, {"prices-backfill"}, {"fundamentals-rebuild", "all"}, {"daily"},
	} {
		run(t, env, append([]string{"jobs", "run"}, job...)...)
	}
	run(t, env, "ingest", "company", "KO")
	run(t, env, "auth", "token", "create", "roles-test")
	run(t, env, "auth", "token", "revoke", "roles-test")
	run(t, env, "auth", "sessions", "revoke-all")

	app, err := pgx.Connect(t.Context(), appURL)
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	defer func() { _ = app.Close(t.Context()) }()
	var facts int
	if err := app.QueryRow(t.Context(), "SELECT count(*) FROM facts").Scan(&facts); err != nil || facts == 0 {
		t.Fatalf("facts as the app role: %d rows, %v; want the rows the jobs loaded", facts, err)
	}

	for _, stmt := range []string{
		"UPDATE facts SET value = value",
		"DELETE FROM facts",
		"TRUNCATE facts",
		"ALTER TABLE facts DISABLE TRIGGER facts_append_only",
		"DROP TRIGGER facts_append_only ON facts",
		"CREATE TABLE app_owned (id integer)",
		"DELETE FROM goose_db_version",
	} {
		_, err := app.Exec(t.Context(), stmt)
		if pgErr := (*pgconn.PgError)(nil); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("%s as the app role: error = %v, want insufficient_privilege (42501)", stmt, err)
		}
	}

	// A table of a later migration is open to the app role at once.
	ownerExec(t, ownerURL, "CREATE TABLE later_table (id bigserial PRIMARY KEY, note text NOT NULL)")
	if _, err := app.Exec(t.Context(), "INSERT INTO later_table (note) VALUES ('x')"); err != nil {
		t.Errorf("insert into a table that the owner created later: %v", err)
	}

	if out, err := pgtest.Psql(t, ownerURL, rolesScript, map[string]string{"app_role": role}); err != nil {
		t.Errorf("roles.sql a second time: %v\n%s", err, out)
	}
	if _, err := app.Exec(t.Context(), "DELETE FROM facts"); err == nil {
		t.Error("DELETE FROM facts after a second run of roles.sql: want an error")
	}
}

func TestTheRolesScriptRefusesADatabaseWithoutTheSchema(t *testing.T) {
	out, err := pgtest.Psql(t, pgtest.NewEmptyURL(t), rolesScript, map[string]string{"app_role": newRoleName()})
	if err == nil || !strings.Contains(out, "pricedin migrate up") {
		t.Errorf("roles.sql before the migrations: error = %v, output:\n%s\nwant a failure that says to migrate first", err, out)
	}
}

func TestTheRolesScriptRefusesTheOwnerAsTheAppRole(t *testing.T) {
	dbURL := pgtest.NewURL(t)
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgtest.Psql(t, dbURL, rolesScript, map[string]string{"app_role": u.User.Username()})
	if err == nil || !strings.Contains(out, "must be another role") {
		t.Errorf("roles.sql with the owner as the app role: error = %v, output:\n%s\nwant a refusal", err, out)
	}
}
