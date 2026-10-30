// Package httpserver runs HTTP servers with timeouts and graceful shutdown,
// and holds the middleware that every server shares.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	// shutdownTimeout bounds the wait for requests in flight. Fly.io and
	// Docker send SIGKILL 10 seconds after SIGTERM by default.
	shutdownTimeout = 8 * time.Second
)

// NewServer returns a server for h with read, write and idle timeouts set.
func NewServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Unit is one server and the listener it serves on. Name appears in logs and
// errors.
type Unit struct {
	Name     string
	Server   *http.Server
	Listener net.Listener
}

// Run serves every unit until ctx ends or one unit fails, then shuts all of
// them down and waits for the requests in flight, for up to 8 seconds. It
// returns nil when ctx ended and every unit shut down cleanly. Otherwise it
// returns the errors, each prefixed with its unit's name. Run closes every
// listener.
func Run(ctx context.Context, logger *slog.Logger, units ...Unit) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	// errs holds the serve error of unit i at i, and its shutdown error at
	// len(units)+i.
	errs := make([]error, 2*len(units))
	var wg sync.WaitGroup
	for i, u := range units {
		wg.Go(func() {
			logger.InfoContext(ctx, "http server listening", slog.String("server", u.Name), slog.String("addr", u.Listener.Addr().String()))
			if err := u.Server.Serve(u.Listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs[i] = fmt.Errorf("%s: serve: %w", u.Name, err)
			}
			// One unit that stops for any reason stops the others.
			stop()
		})
	}

	<-ctx.Done()
	logger.InfoContext(ctx, "http servers shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	for i, u := range units {
		if err := u.Server.Shutdown(shutdownCtx); err != nil {
			errs[len(units)+i] = fmt.Errorf("%s: shutdown: %w", u.Name, err)
		}
	}
	wg.Wait()
	return errors.Join(errs...)
}
