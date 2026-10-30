package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

const healthcheckTimeout = 3 * time.Second

// newHealthcheckCmd exists because the distroless image has no curl or wget
// for a container health check.
func newHealthcheckCmd(getenv func(string) string) *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Exit 0 when the API's /healthz answers 200",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if url == "" {
				cfg, err := config.Load(getenv)
				if err != nil {
					return err
				}
				url = defaultHealthURL(cfg.HTTP.Addr)
			}
			return checkHealth(cmd.Context(), url)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "URL to check (default: /healthz on PRICEDIN_HTTP_ADDR)")
	return cmd
}

// defaultHealthURL returns the /healthz URL of a server that listens on addr.
// A wildcard or empty host becomes 127.0.0.1.
func defaultHealthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/healthz"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func checkHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s answered %d", url, resp.StatusCode)
	}
	return nil
}
