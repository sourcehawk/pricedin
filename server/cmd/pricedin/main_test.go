package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func execute(t *testing.T, env map[string]string, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(func(k string) string { return env[k] }, &out)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.ExecuteContext(t.Context())
}

func TestServeRequiresTheDatabaseURL(t *testing.T) {
	err := execute(t, nil, "serve")
	if !errors.Is(err, config.ErrMissing) || !strings.Contains(err.Error(), "PRICEDIN_DATABASE_URL") {
		t.Errorf("serve error = %v, want a missing PRICEDIN_DATABASE_URL", err)
	}
}

func TestServeRejectsInvalidConfiguration(t *testing.T) {
	err := execute(t, map[string]string{
		"PRICEDIN_DATABASE_URL": "postgres://db/pricedin",
		"PRICEDIN_LOG_LEVEL":    "loud",
	}, "serve")
	var verr *config.VarError
	if !errors.As(err, &verr) || verr.Var != "PRICEDIN_LOG_LEVEL" {
		t.Errorf("serve error = %v, want a VarError for PRICEDIN_LOG_LEVEL", err)
	}
}

func TestHealthcheckSucceedsOnOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	if err := execute(t, nil, "healthcheck", "--url", srv.URL+"/healthz"); err != nil {
		t.Errorf("healthcheck error = %v, want nil", err)
	}
}

func TestHealthcheckFailsOnOtherStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if err := execute(t, nil, "healthcheck", "--url", srv.URL); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("healthcheck error = %v, want an error that names status 503", err)
	}
}

func TestHealthcheckFailsWhenNothingListens(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if err := execute(t, nil, "healthcheck", "--url", url); err == nil {
		t.Error("healthcheck error = nil, want a connection error")
	}
}

func TestDefaultHealthURLFollowsTheHTTPAddress(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{":8080", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0:8181", "http://127.0.0.1:8181/healthz"},
		{"[::]:8181", "http://127.0.0.1:8181/healthz"},
		{"10.1.2.3:8080", "http://10.1.2.3:8080/healthz"},
		{"[::1]:8080", "http://[::1]:8080/healthz"},
	}
	for _, tt := range tests {
		if got := defaultHealthURL(tt.addr); got != tt.want {
			t.Errorf("defaultHealthURL(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}
