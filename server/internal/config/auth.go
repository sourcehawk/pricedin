package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Auth configures the owner login, the sessions and the request checks of
// the HTTP API.
type Auth struct {
	// OwnerEmail is the owner's login email in lower case. Empty when no
	// owner is set; then no login succeeds, and only API tokens work.
	OwnerEmail string
	// OwnerPasswordHash is the Argon2id PHC string of the owner's
	// password, from `pricedin auth hash-password`. It is set exactly when
	// OwnerEmail is set.
	OwnerPasswordHash string
	// SessionTTL is how long a login session lasts. Default 30 days, at
	// least one minute.
	SessionTTL time.Duration
	// CookieSecure sets the Secure attribute of the session cookie.
	// Default true; false only for a local stack on plain http.
	CookieSecure bool
	// PublicOrigins are the origins of the web app, such as
	// "https://pricedin.example.com", without a trailing slash. A login
	// and a state-changing request with the session cookie must come from
	// one of them. Empty means that no such request is accepted.
	PublicOrigins []string
	// TrustedProxyHeader names the header that holds the client address,
	// as a trusted proxy in front of the server sets it, such as
	// "Fly-Client-IP". Empty means the remote address of the connection.
	TrustedProxyHeader string
	// TrustedProxyHops is the number of trusted proxies that append to a
	// list header such as X-Forwarded-For: the client address is that many
	// entries from the right. Default 1.
	TrustedProxyHops int
	// ProxySecret is the secret that the web app's proxy sends with each
	// request, together with the browser's address. A request with the
	// secret counts against that address; see auth.ProxyClientIP. At least
	// 32 printable ASCII characters without spaces. Empty means that no
	// request gives its address that way.
	ProxySecret string
}

const (
	envOwnerEmail         = "PRICEDIN_OWNER_EMAIL"
	envOwnerPasswordHash  = "PRICEDIN_OWNER_PASSWORD_HASH" //nolint:gosec // G101: a variable name, not a credential
	envSessionTTL         = "PRICEDIN_SESSION_TTL"
	envCookieSecure       = "PRICEDIN_COOKIE_SECURE"
	envPublicOrigin       = "PRICEDIN_PUBLIC_ORIGIN"
	envTrustedProxyHeader = "PRICEDIN_TRUSTED_PROXY_HEADER"
	envTrustedProxyHops   = "PRICEDIN_TRUSTED_PROXY_HOPS"
	envProxySecret        = "PRICEDIN_PROXY_SECRET" //nolint:gosec // G101: a variable name, not a credential

	minProxySecretLen = 32
)

func loadAuth(getenv func(string) string) (Auth, []error) {
	cfg := Auth{
		OwnerEmail:         strings.ToLower(strings.TrimSpace(getenv(envOwnerEmail))),
		OwnerPasswordHash:  strings.TrimSpace(getenv(envOwnerPasswordHash)),
		SessionTTL:         30 * 24 * time.Hour,
		CookieSecure:       true,
		TrustedProxyHeader: strings.TrimSpace(getenv(envTrustedProxyHeader)),
		TrustedProxyHops:   1,
		ProxySecret:        strings.TrimSpace(getenv(envProxySecret)),
	}
	var errs []error
	switch {
	case cfg.OwnerEmail != "" && cfg.OwnerPasswordHash == "":
		errs = append(errs, &VarError{Var: envOwnerPasswordHash, Err: fmt.Errorf("required when %s is set", envOwnerEmail)})
	case cfg.OwnerEmail == "" && cfg.OwnerPasswordHash != "":
		errs = append(errs, &VarError{Var: envOwnerEmail, Err: fmt.Errorf("required when %s is set", envOwnerPasswordHash)})
	case cfg.OwnerEmail != "":
		if !strings.Contains(cfg.OwnerEmail, "@") {
			errs = append(errs, &VarError{Var: envOwnerEmail, Err: errors.New("want an email address")})
		}
		if !strings.HasPrefix(cfg.OwnerPasswordHash, "$argon2id$") {
			errs = append(errs, &VarError{Var: envOwnerPasswordHash, Err: errors.New("want an Argon2id hash from `pricedin auth hash-password`")})
		}
	}
	if s := getenv(envSessionTTL); s != "" {
		ttl, err := time.ParseDuration(s)
		if err != nil || ttl < time.Minute {
			errs = append(errs, &VarError{Var: envSessionTTL, Err: fmt.Errorf("want a duration of at least 1m, such as 720h, got %q", s)})
		} else {
			cfg.SessionTTL = ttl
		}
	}
	if s := getenv(envCookieSecure); s != "" {
		secure, err := strconv.ParseBool(s)
		if err != nil {
			errs = append(errs, &VarError{Var: envCookieSecure, Err: fmt.Errorf("want true or false, got %q", s)})
		} else {
			cfg.CookieSecure = secure
		}
	}
	for part := range strings.SplitSeq(getenv(envPublicOrigin), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origin, err := parseOrigin(part)
		if err != nil {
			errs = append(errs, &VarError{Var: envPublicOrigin, Err: err})
			continue
		}
		cfg.PublicOrigins = append(cfg.PublicOrigins, origin)
	}
	if s := getenv(envTrustedProxyHops); s != "" {
		hops, err := strconv.Atoi(s)
		if err != nil || hops < 1 {
			errs = append(errs, &VarError{Var: envTrustedProxyHops, Err: fmt.Errorf("want a whole number of 1 or more, got %q", s)})
		} else {
			cfg.TrustedProxyHops = hops
		}
	}
	if h := cfg.TrustedProxyHeader; h != "" && strings.ContainsFunc(h, func(r rune) bool { return !isHeaderNameRune(r) }) {
		errs = append(errs, &VarError{Var: envTrustedProxyHeader, Err: fmt.Errorf("want a header name, such as Fly-Client-IP, got %q", h)})
	}
	// The error leaves out the value, because the value is a secret.
	if p := cfg.ProxySecret; p != "" && (len(p) < minProxySecretLen || strings.ContainsFunc(p, func(r rune) bool { return r <= ' ' || r > '~' })) {
		errs = append(errs, &VarError{Var: envProxySecret, Err: fmt.Errorf("want at least %d printable ASCII characters without spaces, such as the output of `openssl rand -hex 32`", minProxySecretLen)})
	}
	return cfg, errs
}

// parseOrigin returns the origin in the form a browser sends in the Origin
// header: a lower-case scheme and host, the port when it is not the
// default, and nothing else.
func parseOrigin(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("want an origin such as https://pricedin.example.com, got %q", s)
	}
	host := strings.ToLower(u.Host)
	if (u.Scheme == "https" && strings.HasSuffix(host, ":443")) || (u.Scheme == "http" && strings.HasSuffix(host, ":80")) {
		host = host[:strings.LastIndexByte(host, ':')]
	}
	return u.Scheme + "://" + host, nil
}

// isHeaderNameRune reports whether r may appear in an HTTP header name
// (an RFC 9110 token).
func isHeaderNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
	}
}
