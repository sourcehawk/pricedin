package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

const testHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"

func TestLoadAuthDefaults(t *testing.T) {
	got, err := config.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Auth{SessionTTL: 30 * 24 * time.Hour, CookieSecure: true, TrustedProxyHops: 1}
	if diff := cmp.Diff(want, got.Auth); diff != "" {
		t.Errorf("Auth (-want +got):\n%s", diff)
	}
}

func TestLoadAuthReadsEveryVariable(t *testing.T) {
	got, err := config.Load(env(map[string]string{
		"PRICEDIN_OWNER_EMAIL":          " Owner@Example.com ",
		"PRICEDIN_OWNER_PASSWORD_HASH":  testHash,
		"PRICEDIN_SESSION_TTL":          "12h",
		"PRICEDIN_COOKIE_SECURE":        "false",
		"PRICEDIN_PUBLIC_ORIGIN":        "https://pricedin.example.com, http://localhost:3000/",
		"PRICEDIN_TRUSTED_PROXY_HEADER": "X-Forwarded-For",
		"PRICEDIN_TRUSTED_PROXY_HOPS":   "2",
		"PRICEDIN_PROXY_SECRET":         " 0123456789abcdef0123456789abcdef ",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Auth{
		OwnerEmail:         "owner@example.com",
		OwnerPasswordHash:  testHash,
		SessionTTL:         12 * time.Hour,
		CookieSecure:       false,
		PublicOrigins:      []string{"https://pricedin.example.com", "http://localhost:3000"},
		TrustedProxyHeader: "X-Forwarded-For",
		TrustedProxyHops:   2,
		ProxySecret:        "0123456789abcdef0123456789abcdef",
	}
	if diff := cmp.Diff(want, got.Auth); diff != "" {
		t.Errorf("Auth (-want +got):\n%s", diff)
	}
}

func TestLoadAuthRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		vars    map[string]string
		wantVar string
	}{
		{"email without a hash", map[string]string{"PRICEDIN_OWNER_EMAIL": "owner@example.com"}, "PRICEDIN_OWNER_PASSWORD_HASH"},
		{"hash without an email", map[string]string{"PRICEDIN_OWNER_PASSWORD_HASH": testHash}, "PRICEDIN_OWNER_EMAIL"},
		{"email without an at sign", map[string]string{"PRICEDIN_OWNER_EMAIL": "owner", "PRICEDIN_OWNER_PASSWORD_HASH": testHash}, "PRICEDIN_OWNER_EMAIL"},
		{"hash that is not Argon2id", map[string]string{"PRICEDIN_OWNER_EMAIL": "owner@example.com", "PRICEDIN_OWNER_PASSWORD_HASH": "$2y$10$abc"}, "PRICEDIN_OWNER_PASSWORD_HASH"},
		{"TTL that is not a duration", map[string]string{"PRICEDIN_SESSION_TTL": "30 days"}, "PRICEDIN_SESSION_TTL"},
		{"TTL below a minute", map[string]string{"PRICEDIN_SESSION_TTL": "30s"}, "PRICEDIN_SESSION_TTL"},
		{"cookie flag that is not a boolean", map[string]string{"PRICEDIN_COOKIE_SECURE": "sometimes"}, "PRICEDIN_COOKIE_SECURE"},
		{"origin with a path", map[string]string{"PRICEDIN_PUBLIC_ORIGIN": "https://example.com/app"}, "PRICEDIN_PUBLIC_ORIGIN"},
		{"origin without a scheme", map[string]string{"PRICEDIN_PUBLIC_ORIGIN": "example.com"}, "PRICEDIN_PUBLIC_ORIGIN"},
		{"origin with a query", map[string]string{"PRICEDIN_PUBLIC_ORIGIN": "https://example.com?a=1"}, "PRICEDIN_PUBLIC_ORIGIN"},
		{"zero proxy hops", map[string]string{"PRICEDIN_TRUSTED_PROXY_HOPS": "0"}, "PRICEDIN_TRUSTED_PROXY_HOPS"},
		{"proxy hops that are not a number", map[string]string{"PRICEDIN_TRUSTED_PROXY_HOPS": "two"}, "PRICEDIN_TRUSTED_PROXY_HOPS"},
		{"proxy header with a space", map[string]string{"PRICEDIN_TRUSTED_PROXY_HEADER": "Client IP"}, "PRICEDIN_TRUSTED_PROXY_HEADER"},
		{"proxy secret shorter than 32 characters", map[string]string{"PRICEDIN_PROXY_SECRET": "0123456789abcdef0123456789abcde"}, "PRICEDIN_PROXY_SECRET"},
		{"proxy secret with a space", map[string]string{"PRICEDIN_PROXY_SECRET": "0123456789abcdef 0123456789abcdef"}, "PRICEDIN_PROXY_SECRET"},
		{"proxy secret with a non-ASCII character", map[string]string{"PRICEDIN_PROXY_SECRET": "0123456789abcdef0123456789abcdeé"}, "PRICEDIN_PROXY_SECRET"},
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

func TestLoadAuthKeepsTheProxySecretOutOfErrors(t *testing.T) {
	const secret = "short secret"
	_, err := config.Load(env(map[string]string{"PRICEDIN_PROXY_SECRET": secret}))
	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("Load() error = %q, want it without the secret", err)
	}
}
