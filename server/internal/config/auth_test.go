package config_test

import (
	"errors"
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
	want := config.Auth{SessionTTL: 30 * 24 * time.Hour, CookieSecure: true}
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
		"PRICEDIN_TRUSTED_PROXY_HEADER": "Fly-Client-IP",
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
		TrustedProxyHeader: "Fly-Client-IP",
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
		{"proxy header with a space", map[string]string{"PRICEDIN_TRUSTED_PROXY_HEADER": "Client IP"}, "PRICEDIN_TRUSTED_PROXY_HEADER"},
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

func TestAuthOwnerConfigured(t *testing.T) {
	if (config.Auth{}).OwnerConfigured() {
		t.Error("OwnerConfigured() = true without an owner, want false")
	}
	if !(config.Auth{OwnerEmail: "o@example.com", OwnerPasswordHash: testHash}).OwnerConfigured() {
		t.Error("OwnerConfigured() = false with an owner, want true")
	}
}
