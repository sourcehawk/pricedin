package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func newAuthCmd(getenv func(string) string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Make the owner's password hash and manage API tokens",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("auth: want a subcommand: hash-password or token")
		},
	}

	var fromStdin bool
	hash := &cobra.Command{
		Use:   "hash-password",
		Short: "Print the Argon2id hash of a password for PRICEDIN_OWNER_PASSWORD_HASH",
		Long: "Reads the password from the terminal without echo, twice, or with --stdin the first line of standard input, " +
			"and prints its Argon2id hash. The password must be 12 to 1024 bytes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			password, err := readPassword(cmd, fromStdin)
			if err != nil {
				return err
			}
			phc, err := auth.HashPassword(password, auth.DefaultParams())
			if err != nil {
				return fmt.Errorf("auth: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), phc)
			return err
		},
	}
	hash.Flags().BoolVar(&fromStdin, "stdin", false, "read the password from the first line of standard input")

	token := &cobra.Command{
		Use:   "token",
		Short: "Create, list or revoke API tokens",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("auth token: want a subcommand: create, list or revoke")
		},
	}
	token.AddCommand(
		&cobra.Command{
			Use:   "create <name>",
			Short: "Create an API token and print it once",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return withAuth(cmd.Context(), getenv, func(svc *auth.Service) error {
					t, _, err := svc.CreateAPIToken(cmd.Context(), args[0])
					if err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Store this token now: it is shown only once. Send it as Authorization: Bearer <token>.")
					_, err = fmt.Fprintln(cmd.OutOrStdout(), t)
					return err
				})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List the API tokens, revoked ones too",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withAuth(cmd.Context(), getenv, func(svc *auth.Service) error {
					tokens, err := svc.ListAPITokens(cmd.Context())
					if err != nil {
						return err
					}
					w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
					_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tCREATED\tLAST USED")
					for _, t := range tokens {
						status := "active"
						if t.RevokedAt != nil {
							status = "revoked"
						}
						_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Name, status, stamp(&t.CreatedAt), stamp(t.LastUsedAt))
					}
					return w.Flush()
				})
			},
		},
		&cobra.Command{
			Use:   "revoke <name>",
			Short: "Revoke the active API token with the name",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return withAuth(cmd.Context(), getenv, func(svc *auth.Service) error {
					return svc.RevokeAPIToken(cmd.Context(), args[0])
				})
			},
		},
	)

	cmd.AddCommand(hash, token)
	return cmd
}

// readPassword reads the first line of the command's input with fromStdin,
// else the password typed twice at a terminal without echo.
func readPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	in := cmd.InOrStdin()
	if fromStdin {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("auth: read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", errors.New("auth: standard input is not a terminal; pass --stdin to read the password from it")
	}
	read := func(prompt string) (string, error) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(cmd.ErrOrStderr())
		return string(b), err
	}
	first, err := read("Password: ")
	if err != nil {
		return "", fmt.Errorf("auth: read password: %w", err)
	}
	second, err := read("Repeat: ")
	if err != nil {
		return "", fmt.Errorf("auth: read password: %w", err)
	}
	if first != second {
		return "", errors.New("auth: the two passwords differ")
	}
	return first, nil
}

// withAuth runs fn with an auth service over the configured database.
func withAuth(ctx context.Context, getenv func(string) string, fn func(*auth.Service) error) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if err := cfg.Database.Require(); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		// The parse error can repeat the URL, and with it the password.
		return errors.New("auth: the database URL does not parse")
	}
	defer pool.Close()
	svc, err := auth.New(store.New(pool), auth.Options{
		OwnerEmail: cfg.Auth.OwnerEmail, OwnerPasswordHash: cfg.Auth.OwnerPasswordHash, SessionTTL: cfg.Auth.SessionTTL,
	})
	if err != nil {
		return err
	}
	return fn(svc)
}

func stamp(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04Z")
}
