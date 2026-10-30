package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ingest configures the ingestion jobs and the scheduler.
type Ingest struct {
	// Universe, when not empty, narrows the covered universe to these
	// symbols, upper case, in the order given. Empty means every company
	// with a current NYSE or Nasdaq ticker.
	Universe []string
	// PricesBackfillDays is the most trading days, counted back from the
	// last finished session, that prices-backfill loads. Default 1260,
	// about five years.
	PricesBackfillDays int
	// DailyAt is the time of day, in UTC, at which the scheduler runs the
	// daily job, as an offset from midnight. Default 01:30.
	DailyAt time.Duration
}

const (
	envUniverse           = "PRICEDIN_UNIVERSE"
	envPricesBackfillDays = "PRICEDIN_PRICES_BACKFILL_DAYS"
	envScheduleDaily      = "PRICEDIN_SCHEDULE_DAILY"

	defaultPricesBackfillDays = 1260
	defaultDailyAt            = time.Hour + 30*time.Minute
)

// symbolPattern accepts SEC ticker symbols, such as "BRK-B".
var symbolPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]*$`)

func loadIngest(getenv func(string) string) (Ingest, []error) {
	cfg := Ingest{PricesBackfillDays: defaultPricesBackfillDays, DailyAt: defaultDailyAt}
	var errs []error

	if raw := getenv(envUniverse); strings.TrimSpace(raw) != "" {
		seen := map[string]bool{}
		for part := range strings.SplitSeq(raw, ",") {
			symbol := strings.ToUpper(strings.TrimSpace(part))
			if symbol == "" || seen[symbol] {
				continue
			}
			if !symbolPattern.MatchString(symbol) {
				errs = append(errs, &VarError{Var: envUniverse, Err: fmt.Errorf("%q is not a ticker symbol", symbol)})
				continue
			}
			seen[symbol] = true
			cfg.Universe = append(cfg.Universe, symbol)
		}
	}

	if s := getenv(envPricesBackfillDays); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			errs = append(errs, &VarError{Var: envPricesBackfillDays, Err: fmt.Errorf("want a positive number of trading days, got %q", s)})
		} else {
			cfg.PricesBackfillDays = n
		}
	}

	if s := getenv(envScheduleDaily); s != "" {
		at, err := parseTimeOfDay(s)
		if err != nil {
			errs = append(errs, &VarError{Var: envScheduleDaily, Err: err})
		} else {
			cfg.DailyAt = at
		}
	}
	return cfg, errs
}

// parseTimeOfDay reads "HH:MM" from 00:00 to 23:59.
func parseTimeOfDay(s string) (time.Duration, error) {
	hh, mm, ok := strings.Cut(s, ":")
	if ok && len(hh) == 2 && len(mm) == 2 {
		h, errH := strconv.Atoi(hh)
		m, errM := strconv.Atoi(mm)
		if errH == nil && errM == nil && h >= 0 && h < 24 && m >= 0 && m < 60 {
			return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
		}
	}
	return 0, errors.New("want a UTC time of day as HH:MM, such as 01:30")
}
