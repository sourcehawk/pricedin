// Command pricedin runs the PricedIn API server and, in later versions, its
// migrations and ingestion jobs.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRootCmd(os.Getenv, os.Stdout).ExecuteContext(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pricedin:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the command tree. Subcommands read their configuration
// through getenv and write logs to stdout.
func newRootCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "pricedin",
		Short:         "PricedIn API server and jobs",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(
		newServeCmd(getenv, stdout),
		newHealthcheckCmd(getenv),
	)
	return root
}

func newLogger(cfg config.Log, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Format == config.LogFormatText {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}
