// Package config reads the PRICEDIN_ environment variables into a typed
// Config. Each concern has its own section type and its own loader.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
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

// Database configures the Postgres connection. It comes from
// PRICEDIN_DATABASE_URL, or from the PRICEDIN_DATABASE_HOST, _PORT, _USER,
// _PASSWORD, _NAME and _SSLMODE parts, which need no URL encoding. Setting
// both forms is an error.
type Database struct {
	// URL is a URL with the scheme postgres or postgresql, also when it was
	// built from the parts. Empty when neither form is set.
	URL string
}

const (
	envDatabaseURL      = "PRICEDIN_DATABASE_URL"
	envDatabaseHost     = "PRICEDIN_DATABASE_HOST"
	envDatabasePort     = "PRICEDIN_DATABASE_PORT"
	envDatabaseUser     = "PRICEDIN_DATABASE_USER"
	envDatabasePassword = "PRICEDIN_DATABASE_PASSWORD" //nolint:gosec // G101: a variable name, not a credential.
	envDatabaseName     = "PRICEDIN_DATABASE_NAME"
	envDatabaseSSLMode  = "PRICEDIN_DATABASE_SSLMODE"
)

func loadDatabase(getenv func(string) string) (Database, []error) {
	rawURL := getenv(envDatabaseURL)
	parts := map[string]string{}
	for _, name := range []string{envDatabaseHost, envDatabasePort, envDatabaseUser, envDatabasePassword, envDatabaseName, envDatabaseSSLMode} {
		if v := getenv(name); v != "" {
			parts[name] = v
		}
	}

	switch {
	case rawURL != "" && len(parts) > 0:
		return Database{}, []error{&VarError{Var: envDatabaseHost, Err: fmt.Errorf("set either %s or the PRICEDIN_DATABASE_ parts, not both", envDatabaseURL)}}
	case rawURL != "":
		return checkDatabaseURL(rawURL)
	case len(parts) > 0:
		return databaseFromParts(parts)
	default:
		return Database{}, nil
	}
}

func checkDatabaseURL(rawURL string) (Database, []error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// The parse error repeats the URL, and with it the password.
		return Database{}, []error{&VarError{Var: envDatabaseURL, Err: errors.New("not a valid URL")}}
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return Database{}, []error{&VarError{Var: envDatabaseURL, Err: errors.New("want a postgres:// or postgresql:// URL")}}
	}
	return Database{URL: rawURL}, nil
}

func databaseFromParts(parts map[string]string) (Database, []error) {
	var errs []error
	host := parts[envDatabaseHost]
	if host == "" {
		errs = append(errs, &VarError{Var: envDatabaseHost, Err: errors.New("required when another PRICEDIN_DATABASE_ part is set")})
	}
	port := withDefault(parts[envDatabasePort], "5432")
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		errs = append(errs, &VarError{Var: envDatabasePort, Err: fmt.Errorf("want a port from 1 to 65535, got %q", port)})
	}
	if parts[envDatabasePassword] != "" && parts[envDatabaseUser] == "" {
		errs = append(errs, &VarError{Var: envDatabaseUser, Err: fmt.Errorf("required when %s is set", envDatabasePassword)})
	}
	sslMode := parts[envDatabaseSSLMode]
	switch sslMode {
	case "", "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		errs = append(errs, &VarError{Var: envDatabaseSSLMode, Err: fmt.Errorf("unknown sslmode %q", sslMode)})
	}
	if len(errs) > 0 {
		return Database{}, errs
	}

	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port)}
	if user := parts[envDatabaseUser]; user != "" {
		if password, ok := parts[envDatabasePassword]; ok {
			u.User = url.UserPassword(user, password)
		} else {
			u.User = url.User(user)
		}
	}
	if name := parts[envDatabaseName]; name != "" {
		u.Path = "/" + name
	}
	if sslMode != "" {
		u.RawQuery = url.Values{"sslmode": {sslMode}}.Encode()
	}
	return Database{URL: u.String()}, nil
}

// Require returns a *VarError that wraps ErrMissing when neither the URL nor
// the parts are set.
func (d Database) Require() error {
	if d.URL == "" {
		return &VarError{Var: envDatabaseURL, Err: fmt.Errorf("%w: set it, or set %s and the other parts", ErrMissing, envDatabaseHost)}
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
