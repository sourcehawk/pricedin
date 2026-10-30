// Package config reads the PRICEDIN_ environment variables into a typed
// Config. Each concern has its own section type and its own loader.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
)

// Config holds every setting of the pricedin binary.
type Config struct {
	HTTP     HTTP
	Database Database
	Log      Log
}

// Load reads the configuration through getenv, which is os.Getenv outside
// tests. An unset or empty variable takes its default. Load checks the format
// of every value that is set, and returns all the problems it finds joined in
// one error; each one is a *VarError. Load does not require a value that only
// some subcommands need: such a section has a Require method.
func Load(getenv func(string) string) (Config, error) {
	httpCfg, httpErrs := loadHTTP(getenv)
	db, dbErrs := loadDatabase(getenv)
	logCfg, logErrs := loadLog(getenv)

	if errs := concat(httpErrs, dbErrs, logErrs); len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return Config{HTTP: httpCfg, Database: db, Log: logCfg}, nil
}

// VarError reports an environment variable that is missing or invalid.
type VarError struct {
	Var string
	Err error
}

func (e *VarError) Error() string {
	return fmt.Sprintf("config: %s: %v", e.Var, e.Err)
}

func (e *VarError) Unwrap() error { return e.Err }

// ErrMissing is the cause of a VarError for a variable that a subcommand
// requires and that is not set.
var ErrMissing = errors.New("required but not set")

// HTTP configures the API listener and the separate metrics listener.
type HTTP struct {
	// Addr is the host:port of the API listener. Default ":8080".
	Addr string
	// MetricsAddr is the host:port of the Prometheus /metrics listener.
	// Default ":9090". It must differ from Addr.
	MetricsAddr string
}

const (
	envHTTPAddr    = "PRICEDIN_HTTP_ADDR"
	envMetricsAddr = "PRICEDIN_METRICS_ADDR"
)

func loadHTTP(getenv func(string) string) (HTTP, []error) {
	cfg := HTTP{
		Addr:        withDefault(getenv(envHTTPAddr), ":8080"),
		MetricsAddr: withDefault(getenv(envMetricsAddr), ":9090"),
	}
	var errs []error
	if err := checkHostPort(cfg.Addr); err != nil {
		errs = append(errs, &VarError{Var: envHTTPAddr, Err: err})
	}
	if err := checkHostPort(cfg.MetricsAddr); err != nil {
		errs = append(errs, &VarError{Var: envMetricsAddr, Err: err})
	} else if cfg.MetricsAddr == cfg.Addr {
		errs = append(errs, &VarError{Var: envMetricsAddr, Err: fmt.Errorf("must differ from %s", envHTTPAddr)})
	}
	return cfg, errs
}

func checkHostPort(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("want host:port: %w", err)
	}
	return nil
}

// Database configures the Postgres connection.
type Database struct {
	// URL is a postgres:// or postgresql:// connection URL. Empty when unset.
	URL string
}

const envDatabaseURL = "PRICEDIN_DATABASE_URL"

func loadDatabase(getenv func(string) string) (Database, []error) {
	cfg := Database{URL: getenv(envDatabaseURL)}
	if cfg.URL == "" {
		return cfg, nil
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		// The parse error repeats the URL, and with it the password.
		return cfg, []error{&VarError{Var: envDatabaseURL, Err: errors.New("not a valid URL")}}
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return cfg, []error{&VarError{Var: envDatabaseURL, Err: errors.New("want a postgres:// or postgresql:// URL")}}
	}
	return cfg, nil
}

// Require returns a *VarError that wraps ErrMissing when the URL is not set.
func (d Database) Require() error {
	if d.URL == "" {
		return &VarError{Var: envDatabaseURL, Err: ErrMissing}
	}
	return nil
}

// LogFormat selects the slog handler.
type LogFormat string

// The log formats that PRICEDIN_LOG_FORMAT accepts.
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Log configures the structured logger.
type Log struct {
	// Level is the lowest level written. Default info.
	Level slog.Level
	// Format is json (default) or text.
	Format LogFormat
}

const (
	envLogLevel  = "PRICEDIN_LOG_LEVEL"
	envLogFormat = "PRICEDIN_LOG_FORMAT"
)

func loadLog(getenv func(string) string) (Log, []error) {
	var errs []error
	cfg := Log{Level: slog.LevelInfo, Format: LogFormatJSON}

	switch level := strings.ToLower(getenv(envLogLevel)); level {
	case "":
	case "debug", "info", "warn", "error":
		// UnmarshalText cannot fail on these four names.
		_ = cfg.Level.UnmarshalText([]byte(level))
	default:
		errs = append(errs, &VarError{Var: envLogLevel, Err: fmt.Errorf("unknown level %q, want debug, info, warn or error", level)})
	}

	switch format := LogFormat(strings.ToLower(getenv(envLogFormat))); format {
	case "":
	case LogFormatJSON, LogFormatText:
		cfg.Format = format
	default:
		errs = append(errs, &VarError{Var: envLogFormat, Err: fmt.Errorf("unknown format %q, want json or text", format)})
	}

	return cfg, errs
}

func concat(lists ...[]error) []error {
	var all []error
	for _, l := range lists {
		all = append(all, l...)
	}
	return all
}

func withDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
