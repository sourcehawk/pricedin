package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
)

func newPricesCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prices",
		Short: "Work with the price provider",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Check the live Massive API: the NVDA split, KO dividends and the GRMN CIK",
		Long: "verify calls the live Massive API with PRICEDIN_MASSIVE_API_KEY. It prints the raw\n" +
			"NVDA closes around the 10-for-1 split of 2024-06-10, the closes that AdjustForSplits\n" +
			"computes and Massive's own adjusted closes, the last four KO dividends, and the GRMN\n" +
			"CIK compared with the SEC value. It exits with an error when a check fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(getenv)
			if err != nil {
				return err
			}
			if err := cfg.Massive.Require(); err != nil {
				return err
			}
			client, err := massive.New(massive.Options{
				BaseURL:   cfg.Massive.BaseURL,
				APIKey:    cfg.Massive.APIKey,
				RateLimit: cfg.Massive.RateLimit,
			})
			if err != nil {
				return err
			}
			return verifyPrices(cmd.Context(), stdout, client, time.Now())
		},
	})
	return cmd
}

const (
	nvdaSplitRatio = 10
	// grmnSECCIK is GRMN's CIK in SEC EDGAR.
	grmnSECCIK = domain.CIK(1121788)
	// closeTolerance allows for Massive's rounding of adjusted closes.
	closeTolerance = 0.005
)

// verifyPrices runs the checks of prices verify and writes the report to w.
// It returns an error when a check fails or a request fails.
func verifyPrices(ctx context.Context, w io.Writer, c *massive.Client, now time.Time) error {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	today := domain.DateOf(now.In(newYork))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var failed []string
	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		failed = append(failed, msg)
		_, _ = fmt.Fprintf(tw, "  FAILED: %s\n", msg)
	}

	_, _ = fmt.Fprintf(tw, "Massive price checks on %s\n\n", today)

	if err := verifySplit(ctx, tw, c, today, fail); err != nil {
		return err
	}
	if err := verifyDividends(ctx, tw, c, today, fail); err != nil {
		return err
	}
	if err := verifyCIK(ctx, tw, c, fail); err != nil {
		return err
	}

	if len(failed) == 0 {
		_, _ = fmt.Fprintln(tw, "All checks passed.")
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("prices verify: write: %w", err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("prices verify: %d check(s) failed: %v", len(failed), failed)
	}
	return nil
}

func verifySplit(ctx context.Context, w io.Writer, c *massive.Client, today domain.Date, fail func(string, ...any)) error {
	_, _ = fmt.Fprintln(w, "NVDA split")
	nvdaSplitDate := domain.NewDate(2024, time.June, 10)
	nvdaFrom, nvdaTo := nvdaSplitDate.AddDays(-5), nvdaSplitDate.AddDays(2)
	splits, err := c.Splits(ctx, "NVDA")
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	found := false
	for _, s := range splits {
		if s.Date == nvdaSplitDate {
			found = true
			_, _ = fmt.Fprintf(w, "  Split event:\t%s\t%g -> %g\t(ratio %g)\tid %s\n", s.Date, s.From, s.To, s.Ratio(), s.Evidence)
			if s.Ratio() != nvdaSplitRatio {
				fail("the NVDA split of %s has ratio %g, want %d", s.Date, s.Ratio(), nvdaSplitRatio)
			}
		}
	}
	if !found {
		fail("no NVDA split on %s among %d split events", nvdaSplitDate, len(splits))
	}

	raw, err := c.DailyBars(ctx, "NVDA", nvdaFrom, nvdaTo)
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	theirs, err := c.AdjustedDailyBars(ctx, "NVDA", nvdaFrom, nvdaTo)
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	ours, err := prices.AdjustForSplits(raw, splits, today)
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	massiveClose := map[domain.Date]float64{}
	for _, b := range theirs {
		massiveClose[b.Date] = b.Close
	}

	_, _ = fmt.Fprintln(w, "  Date\tRaw close\tFactor\tAdjustForSplits\tMassive adjusted\tMatch")
	var before, after int
	for _, b := range ours {
		if b.Bar.Date.Before(nvdaSplitDate) {
			before++
		} else {
			after++
		}
		want, ok := massiveClose[b.Bar.Date]
		match := ok && math.Abs(b.Close-want) <= closeTolerance
		_, _ = fmt.Fprintf(w, "  %s\t%.4f\t%g\t%.4f\t%s\t%s\n", b.Bar.Date, b.Bar.Close, b.Factor, b.Close, formatClose(want, ok), yesNo(match))
		if !match {
			fail("the NVDA close of %s adjusted by AdjustForSplits does not match Massive's adjusted close", b.Bar.Date)
		}
	}
	// Only a bar before the split shows that the adjustment divides by the
	// ratio; after it, every factor is 1.
	if before == 0 || after == 0 {
		fail("want NVDA bars before the split and from %s on, got %d before the split and %d after", nvdaSplitDate, before, after)
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

func verifyDividends(ctx context.Context, w io.Writer, c *massive.Client, today domain.Date, fail func(string, ...any)) error {
	_, _ = fmt.Fprintf(w, "KO dividends: the last four with an ex-dividend date on or before %s\n", today)
	divs, err := c.Dividends(ctx, "KO")
	if err != nil {
		return fmt.Errorf("prices verify: %w", err)
	}
	var past []domain.Dividend
	for i := range divs {
		if !divs[i].ExDate.After(today) {
			past = append(past, divs[i])
		}
	}
	past = past[max(0, len(past)-4):]
	if len(past) == 0 {
		fail("no KO dividends")
	} else {
		_, _ = fmt.Fprintln(w, "  Ex-date\tPay date\tAmount\tCurrency\tFrequency\tType")
	}
	for i := range past {
		d := &past[i]
		pay := "-"
		if d.PayDate != nil {
			pay = d.PayDate.String()
		}
		_, _ = fmt.Fprintf(w, "  %s\t%s\t%.4f\t%s\t%d\t%s\n", d.ExDate, pay, d.CashAmount, d.Currency, d.Frequency, d.Type)
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

func verifyCIK(ctx context.Context, w io.Writer, c *massive.Client, fail func(string, ...any)) error {
	_, _ = fmt.Fprintln(w, "GRMN CIK")
	t, err := c.Ticker(ctx, "GRMN")
	var apiErr *massive.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Status == "NOT_FOUND":
		fail("Massive does not know GRMN")
	case err != nil:
		return fmt.Errorf("prices verify: %w", err)
	default:
		_, _ = fmt.Fprintf(w, "  Massive: %s\n  SEC:     %s\n", t.CIK, grmnSECCIK)
		if t.CIK != grmnSECCIK {
			fail("the GRMN CIK from Massive does not match the SEC value")
		}
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

func formatClose(v float64, ok bool) string {
	if !ok {
		return "missing"
	}
	return fmt.Sprintf("%.4f", v)
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
