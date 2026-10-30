// Command fixtureserver serves the SEC and Massive fixtures in testdata at
// the paths of the real services, for Compose and the end-to-end tests. See
// package fixtureserver for the paths.
//
// Usage:
//
//	fixtureserver [-addr :8090] [-root testdata] [-massive-key fixture-key]
//	fixtureserver -check http://127.0.0.1:8090/healthz
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/fixtureserver"
	"github.com/sourcehawk/pricedin/server/internal/httpserver"
)

func main() {
	addr := flag.String("addr", ":8090", "listen address")
	root := flag.String("root", "testdata", "the testdata folder")
	key := flag.String("massive-key", "fixture-key", "the bearer token that the Massive paths require")
	check := flag.String("check", "", "only GET this URL and exit 0 when it answers 200, for a container health check")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if *check != "" {
		err = healthcheck(ctx, *check)
	} else {
		err = serve(ctx, *addr, *root, *key)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixtureserver:", err)
		stop()
		os.Exit(1) //nolint:gocritic // stop already ran
	}
}

func serve(ctx context.Context, addr, root, key string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	h, err := fixtureserver.New(fixtureserver.Options{Root: root, MassiveAPIKey: key})
	if err != nil {
		return err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "fixtureserver: listening", "addr", ln.Addr().String(), "root", root)
	return httpserver.Run(ctx, logger, httpserver.Unit{Name: "fixtures", Server: httpserver.NewServer(h), Listener: ln})
}

func healthcheck(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}
