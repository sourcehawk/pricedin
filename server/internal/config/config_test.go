package config_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadDefaults(t *testing.T) {
	got, err := config.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Config{
		HTTP:    config.HTTP{Addr: ":8080", MetricsAddr: ":9090"},
		Log:     config.Log{Level: slog.LevelInfo, Format: config.LogFormatJSON},
		Massive: config.Massive{BaseURL: "https://api.massive.com", RateLimit: 20},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadReadsEveryVariable(t *testing.T) {
	got, err := config.Load(env(map[string]string{
		"PRICEDIN_HTTP_ADDR":          "127.0.0.1:8181",
		"PRICEDIN_METRICS_ADDR":       "127.0.0.1:9191",
		"PRICEDIN_DATABASE_URL":       "postgres://u:p@db:5432/pricedin?sslmode=disable",
		"PRICEDIN_LOG_LEVEL":          "debug",
		"PRICEDIN_LOG_FORMAT":         "text",
		"PRICEDIN_MASSIVE_BASE_URL":   "http://fixtures:8090",
		"PRICEDIN_MASSIVE_API_KEY":    "test-key",
		"PRICEDIN_MASSIVE_RATE_LIMIT": "2.5",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Config{
		HTTP:     config.HTTP{Addr: "127.0.0.1:8181", MetricsAddr: "127.0.0.1:9191"},
		Database: config.Database{URL: "postgres://u:p@db:5432/pricedin?sslmode=disable"},
		Log:      config.Log{Level: slog.LevelDebug, Format: config.LogFormatText},
		Massive:  config.Massive{BaseURL: "http://fixtures:8090", APIKey: "test-key", RateLimit: 2.5},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadAcceptsEveryLogLevel(t *testing.T) {
	tests := []struct {
		value string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"WARN", slog.LevelWarn},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := config.Load(env(map[string]string{"PRICEDIN_LOG_LEVEL": tt.value}))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Log.Level != tt.want {
				t.Errorf("Log.Level = %v, want %v", got.Log.Level, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		vars    map[string]string
		wantVar string
	}{
		{"http addr without port", map[string]string{"PRICEDIN_HTTP_ADDR": "localhost"}, "PRICEDIN_HTTP_ADDR"},
		{"metrics addr without port", map[string]string{"PRICEDIN_METRICS_ADDR": "9090"}, "PRICEDIN_METRICS_ADDR"},
		{"same http and metrics addr", map[string]string{"PRICEDIN_HTTP_ADDR": ":8080", "PRICEDIN_METRICS_ADDR": ":8080"}, "PRICEDIN_METRICS_ADDR"},
		{"unknown log level", map[string]string{"PRICEDIN_LOG_LEVEL": "verbose"}, "PRICEDIN_LOG_LEVEL"},
		{"unknown log format", map[string]string{"PRICEDIN_LOG_FORMAT": "xml"}, "PRICEDIN_LOG_FORMAT"},
		{"database url with other scheme", map[string]string{"PRICEDIN_DATABASE_URL": "mysql://db/pricedin"}, "PRICEDIN_DATABASE_URL"},
		{"database url that is not a url", map[string]string{"PRICEDIN_DATABASE_URL": "host=db dbname=pricedin"}, "PRICEDIN_DATABASE_URL"},
		{"database url and host together", map[string]string{"PRICEDIN_DATABASE_URL": "postgres://db/pricedin", "PRICEDIN_DATABASE_HOST": "db"}, "PRICEDIN_DATABASE_HOST"},
		{"database part without a host", map[string]string{"PRICEDIN_DATABASE_PASSWORD": "secret"}, "PRICEDIN_DATABASE_HOST"},
		{"database port that is not a number", map[string]string{"PRICEDIN_DATABASE_HOST": "db", "PRICEDIN_DATABASE_PORT": "postgres"}, "PRICEDIN_DATABASE_PORT"},
		{"database port out of range", map[string]string{"PRICEDIN_DATABASE_HOST": "db", "PRICEDIN_DATABASE_PORT": "70000"}, "PRICEDIN_DATABASE_PORT"},
		{"database password without a user", map[string]string{"PRICEDIN_DATABASE_HOST": "db", "PRICEDIN_DATABASE_PASSWORD": "secret"}, "PRICEDIN_DATABASE_USER"},
		{"unknown database sslmode", map[string]string{"PRICEDIN_DATABASE_HOST": "db", "PRICEDIN_DATABASE_SSLMODE": "always"}, "PRICEDIN_DATABASE_SSLMODE"},
		{"massive base url with other scheme", map[string]string{"PRICEDIN_MASSIVE_BASE_URL": "ftp://api.massive.com"}, "PRICEDIN_MASSIVE_BASE_URL"},
		{"massive base url without host", map[string]string{"PRICEDIN_MASSIVE_BASE_URL": "https://"}, "PRICEDIN_MASSIVE_BASE_URL"},
		{"massive base url with a query", map[string]string{"PRICEDIN_MASSIVE_BASE_URL": "https://api.massive.com?apiKey=x"}, "PRICEDIN_MASSIVE_BASE_URL"},
		{"massive rate limit that is not a number", map[string]string{"PRICEDIN_MASSIVE_RATE_LIMIT": "fast"}, "PRICEDIN_MASSIVE_RATE_LIMIT"},
		{"massive rate limit of zero", map[string]string{"PRICEDIN_MASSIVE_RATE_LIMIT": "0"}, "PRICEDIN_MASSIVE_RATE_LIMIT"},
		{"negative massive rate limit", map[string]string{"PRICEDIN_MASSIVE_RATE_LIMIT": "-1"}, "PRICEDIN_MASSIVE_RATE_LIMIT"},
		{"infinite massive rate limit", map[string]string{"PRICEDIN_MASSIVE_RATE_LIMIT": "Inf"}, "PRICEDIN_MASSIVE_RATE_LIMIT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(env(tt.vars))
			var verr *config.VarError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a *config.VarError", err)
			}
			if verr.Var != tt.wantVar {
				t.Errorf("VarError.Var = %q, want %q", verr.Var, tt.wantVar)
			}
		})
	}
}

func TestLoadReportsEveryInvalidVariable(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"PRICEDIN_LOG_LEVEL":  "verbose",
		"PRICEDIN_LOG_FORMAT": "xml",
	}))
	var got []string
	for _, e := range unwrapAll(err) {
		var verr *config.VarError
		if errors.As(e, &verr) {
			got = append(got, verr.Var)
		}
	}
	want := []string{"PRICEDIN_LOG_LEVEL", "PRICEDIN_LOG_FORMAT"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("invalid variables mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadBuildsTheDatabaseURLFromParts(t *testing.T) {
	const password = "p#ss/w@rd:x?y%z $"
	got, err := config.Load(env(map[string]string{
		"PRICEDIN_DATABASE_HOST":     "db",
		"PRICEDIN_DATABASE_PORT":     "5433",
		"PRICEDIN_DATABASE_USER":     "price din",
		"PRICEDIN_DATABASE_PASSWORD": password,
		"PRICEDIN_DATABASE_NAME":     "pricedin/dev",
		"PRICEDIN_DATABASE_SSLMODE":  "disable",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := got.Database.Require(); err != nil {
		t.Errorf("Require() error = %v, want nil", err)
	}

	// pgx is the consumer of the URL, so it decides whether the escaping
	// is right.
	pg, err := pgconn.ParseConfig(got.Database.URL)
	if err != nil {
		t.Fatalf("pgconn.ParseConfig(%q) error = %v", got.Database.URL, err)
	}
	if pg.Host != "db" || pg.Port != 5433 || pg.User != "price din" || pg.Password != password || pg.Database != "pricedin/dev" || pg.TLSConfig != nil {
		t.Errorf("pgx reads host %q, port %d, user %q, password %q, database %q, TLS %t; want db, 5433, %q, %q, %q, no TLS",
			pg.Host, pg.Port, pg.User, pg.Password, pg.Database, pg.TLSConfig != nil, "price din", password, "pricedin/dev")
	}
}

func TestLoadDatabasePartsDefaultToPort5432(t *testing.T) {
	got, err := config.Load(env(map[string]string{"PRICEDIN_DATABASE_HOST": "db", "PRICEDIN_DATABASE_NAME": "pricedin"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	pg, err := pgconn.ParseConfig(got.Database.URL)
	if err != nil {
		t.Fatalf("pgconn.ParseConfig(%q) error = %v", got.Database.URL, err)
	}
	if pg.Port != 5432 {
		t.Errorf("port = %d, want 5432", pg.Port)
	}
}

func TestDatabaseRequire(t *testing.T) {
	if err := (config.Database{URL: "postgres://db/pricedin"}).Require(); err != nil {
		t.Errorf("Require() with a URL error = %v, want nil", err)
	}
	err := config.Database{}.Require()
	var verr *config.VarError
	if !errors.As(err, &verr) || verr.Var != "PRICEDIN_DATABASE_URL" {
		t.Errorf("Require() without a URL error = %v, want a VarError for PRICEDIN_DATABASE_URL", err)
	}
}

func TestMassiveRequire(t *testing.T) {
	if err := (config.Massive{APIKey: "test-key"}).Require(); err != nil {
		t.Errorf("Require() with a key error = %v, want nil", err)
	}
	err := config.Massive{}.Require()
	var verr *config.VarError
	if !errors.As(err, &verr) || verr.Var != "PRICEDIN_MASSIVE_API_KEY" || !errors.Is(err, config.ErrMissing) {
		t.Errorf("Require() without a key error = %v, want a VarError for PRICEDIN_MASSIVE_API_KEY that wraps ErrMissing", err)
	}
}

func unwrapAll(err error) []error {
	if err == nil {
		return nil
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		return joined.Unwrap()
	}
	return []error{err}
}
