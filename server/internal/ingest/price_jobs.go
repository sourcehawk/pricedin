package ingest

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// pricesDailyLookback is the number of calendar days that prices-daily
// checks for missing days, besides every day after the newest loaded one.
const pricesDailyLookback = 30

// providerHistoryDays is the provider's history window in calendar days,
// which a per-symbol load asks for in full.
const providerHistoryDays = 5 * 365

// PriceReferenceResult counts what price-reference wrote.
type PriceReferenceResult struct {
	ProviderTickers int64 // tickers in the provider's list
	Matched         int64 // provider tickers whose CIK is a stored company
	// Opened, Closed and Updated count the provider's symbol rows.
	Opened, Closed, Updated int64
	Symbols                 int64 // universe symbols whose events were fetched
	Splits                  int64 // split events added or changed
	Dividends               int64 // dividends added or changed
	Failed                  int64
}

// Stats implements Result.
func (p PriceReferenceResult) Stats() map[string]int64 {
	return map[string]int64{
		"provider_tickers": p.ProviderTickers, "matched": p.Matched, "opened": p.Opened, "closed": p.Closed,
		"updated": p.Updated, "symbols": p.Symbols, "splits": p.Splits, "dividends": p.Dividends, "failed": p.Failed,
	}
}

// PriceReference syncs the provider's ticker list as the dated symbol map
// of the provider's source, and loads the split events and the cash
// dividends of every universe symbol. The provider's list keeps the
// provider's CIK; a ticker whose CIK is not a stored company is left out.
func (r *Runner) PriceReference(ctx context.Context) (PriceReferenceResult, error) {
	if err := r.needPrices(JobPriceReference); err != nil {
		return PriceReferenceResult{}, err
	}
	return runJob(ctx, r, JobPriceReference, JobPriceReference, false, r.priceReference)
}

func (r *Runner) priceReference(ctx context.Context) (PriceReferenceResult, error) {
	members, err := r.universe(ctx)
	if err != nil {
		return PriceReferenceResult{}, err
	}
	res, err := r.syncProviderTickers(ctx)
	if err != nil {
		return res, err
	}
	var fails failures
	for _, m := range members {
		one, err := r.symbolReference(ctx, m.Symbol)
		if err != nil {
			if ctx.Err() != nil {
				return res, err
			}
			res.Failed++
			fails.add(m.Symbol, err)
			r.logger.WarnContext(ctx, "ingest: price reference failed", "symbol", m.Symbol, "error", err.Error())
			continue
		}
		res.Symbols++
		res.Splits += one.Splits
		res.Dividends += one.Dividends
	}
	return res, fails.err(len(members))
}

func (r *Runner) syncProviderTickers(ctx context.Context) (PriceReferenceResult, error) {
	listCtx, cancel := context.WithTimeout(ctx, tickerListTimeout)
	list, err := r.prices.Tickers(listCtx)
	cancel()
	if err != nil {
		return PriceReferenceResult{}, fmt.Errorf("provider tickers: %w", err)
	}
	ciks, err := r.store.ListCompanyCIKs(ctx)
	if err != nil {
		return PriceReferenceResult{}, err
	}
	res := PriceReferenceResult{ProviderTickers: int64(len(list))}
	current := make([]domain.Ticker, 0, len(list))
	for _, t := range list {
		if t.CIK <= 0 {
			continue
		}
		if _, ok := slices.BinarySearch(ciks, t.CIK); !ok {
			continue
		}
		current = append(current, domain.Ticker{Symbol: SECSymbol(t.Symbol), CIK: t.CIK, Exchange: t.Exchange})
	}
	res.Matched = int64(len(current))
	changes, err := r.store.SyncTickers(ctx, r.prices.Name(), r.today(), current)
	res.Opened, res.Closed, res.Updated = changes.Opened, changes.Closed, changes.Updated
	return res, err
}

// symbolReference loads the split events and dividends of one SEC symbol,
// and marks the symbol as checked. When its splits changed, or it was
// checked for the first time, it marks the quarters of its company
// pending.
func (r *Runner) symbolReference(ctx context.Context, symbol string) (PriceReferenceResult, error) {
	provider := ProviderSymbol(symbol)
	callCtx, cancel := context.WithTimeout(ctx, pricesCallTimeout)
	defer cancel()
	splits, err := r.prices.Splits(callCtx, provider)
	if err != nil {
		return PriceReferenceResult{}, fmt.Errorf("splits: %w", err)
	}
	dividends, err := r.prices.Dividends(callCtx, provider)
	if err != nil {
		return PriceReferenceResult{}, fmt.Errorf("dividends: %w", err)
	}
	for i := range splits {
		splits[i].Symbol = symbol
	}
	for i := range dividends {
		dividends[i].Symbol = symbol
	}
	var res PriceReferenceResult
	err = r.store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		if res.Splits, err = tx.UpsertPriceSplits(ctx, splits); err != nil {
			return err
		}
		if res.Dividends, err = tx.UpsertDividends(ctx, dividends); err != nil {
			return err
		}
		marked, err := tx.MarkPriceReferenceChecked(ctx, []string{symbol})
		if err != nil || (res.Splits == 0 && marked == 0) {
			return err
		}
		return tx.MarkFundamentalsPendingForSymbols(ctx, []string{symbol})
	})
	return res, err
}

// PricesResult counts what a grouped daily load wrote.
type PricesResult struct {
	Days          int64 // trading days without a grouped load before the run
	Loaded        int64 // days loaded now
	Empty         int64 // days for which the provider returned no bars yet
	Bars          int64 // universe bars added or changed
	NotAuthorized int64 // days older than the provider's plan allows
	Failed        int64
	// SymbolsFilled counts the universe symbols that lacked bars on loaded
	// days and got them from a per-symbol request, and SymbolBars those
	// bars.
	SymbolsFilled, SymbolBars int64
}

// Stats implements Result.
func (p PricesResult) Stats() map[string]int64 {
	return map[string]int64{
		"days": p.Days, "loaded": p.Loaded, "empty": p.Empty, "bars": p.Bars,
		"not_authorized": p.NotAuthorized, "failed": p.Failed,
		"symbols_filled": p.SymbolsFilled, "symbol_bars": p.SymbolBars,
	}
}

// PricesBackfill loads the grouped daily bars of each of the last
// PricesBackfillDays trading days up to the last finished session that has
// no grouped load yet, newest first, and stores the bars of universe
// symbols. It stops at the first day that the provider's plan does not
// cover. A day loaded for one company only counts as missing. Then, unless
// the grouped load failed, each universe symbol that lacks a bar on a
// loaded day, such as one added to PRICEDIN_UNIVERSE after those days were
// loaded, gets its bars from a per-symbol request; a symbol that did not
// trade on such days, such as a recent listing, asks again on every
// backfill.
func (r *Runner) PricesBackfill(ctx context.Context) (PricesResult, error) {
	if err := r.needPrices(JobPricesBackfill); err != nil {
		return PricesResult{}, err
	}
	return runJob(ctx, r, JobPricesBackfill, JobPricesBackfill, false, func(ctx context.Context) (PricesResult, error) {
		last := prices.LastFinishedSession(r.now())
		// Two calendar days per trading day is more than enough.
		days := prices.TradingDays(last.AddDays(-2*r.backfillDays-10), last)
		days = days[max(0, len(days)-r.backfillDays):]
		res, err := r.loadGrouped(ctx, days)
		if err != nil {
			return res, err
		}
		err = r.fillSymbols(ctx, days, &res)
		return res, err
	})
}

// fillSymbols gives each universe symbol the bars it lacks on the days of
// days whose grouped bars were loaded.
func (r *Runner) fillSymbols(ctx context.Context, days []domain.Date, res *PricesResult) error {
	members, err := r.universe(ctx)
	if err != nil {
		return err
	}
	missing, err := r.store.ListMissingGroupedPriceDates(ctx, days)
	if err != nil {
		return err
	}
	loaded := slices.DeleteFunc(slices.Clone(days), func(d domain.Date) bool {
		_, found := slices.BinarySearchFunc(missing, d, func(a, b domain.Date) int { return a.Time().Compare(b.Time()) })
		return found
	})
	var fails failures
	for _, m := range members {
		gaps, err := r.store.ListMissingPriceDatesForSymbol(ctx, m.Symbol, loaded)
		if err != nil {
			return err
		}
		if len(gaps) == 0 {
			continue
		}
		n, err := r.symbolBarsFrom(ctx, m.Symbol, gaps[0])
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			res.Failed++
			fails.add(m.Symbol, err)
			continue
		}
		res.SymbolsFilled++
		res.SymbolBars += n
	}
	return fails.err(len(members))
}

// PricesDaily loads the grouped daily bars of every trading day without a
// grouped load from 30 days back, or from the oldest loaded day when that
// is older, to the last finished session, newest first. So one run fills
// the days that earlier runs missed, also a day that failed during a
// catch-up.
func (r *Runner) PricesDaily(ctx context.Context) (PricesResult, error) {
	if err := r.needPrices(JobPricesDaily); err != nil {
		return PricesResult{}, err
	}
	return runJob(ctx, r, JobPricesDaily, JobPricesDaily, false, func(ctx context.Context) (PricesResult, error) {
		last := prices.LastFinishedSession(r.now())
		from := last.AddDays(-pricesDailyLookback)
		oldest, err := r.store.GetEarliestGroupedPriceDate(ctx)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return PricesResult{}, err
		case oldest.Before(from):
			from = oldest
		}
		return r.loadGrouped(ctx, prices.TradingDays(from, last))
	})
}

func (r *Runner) loadGrouped(ctx context.Context, days []domain.Date) (PricesResult, error) {
	members, err := r.universe(ctx)
	if err != nil {
		return PricesResult{}, err
	}
	keep := map[string]bool{}
	for _, m := range members {
		keep[m.Symbol] = true
	}
	missing, err := r.store.ListMissingGroupedPriceDates(ctx, days)
	if err != nil {
		return PricesResult{}, err
	}
	return r.loadGroupedDays(ctx, missing, keep)
}

// loadGroupedDays loads the days of missing, newest first.
func (r *Runner) loadGroupedDays(ctx context.Context, missing []domain.Date, keep map[string]bool) (PricesResult, error) {
	missing = slices.Clone(missing)
	slices.Reverse(missing)
	res := PricesResult{Days: int64(len(missing))}
	var fails failures
	for _, d := range missing {
		n, bars, err := r.loadGroupedDay(ctx, d, keep)
		switch {
		case errors.Is(err, prices.ErrNotAuthorized):
			// Older days are outside the plan too.
			res.NotAuthorized = int64(len(missing)) - res.Loaded - res.Empty - res.Failed
			r.logger.InfoContext(ctx, "ingest: the provider's plan ends here", "date", d)
			return res, fails.err(len(missing))
		case err != nil:
			if ctx.Err() != nil {
				return res, err
			}
			res.Failed++
			fails.add(d.String(), err)
			continue
		case n == 0:
			res.Empty++
		default:
			res.Loaded++
		}
		res.Bars += bars
	}
	return res, fails.err(len(missing))
}

// loadGroupedDay stores the universe bars of day d and marks the day
// loaded when the provider returned any bar. It returns the number of bars
// the provider returned and the number of bars stored.
func (r *Runner) loadGroupedDay(ctx context.Context, d domain.Date, keep map[string]bool) (int, int64, error) {
	callCtx, cancel := context.WithTimeout(ctx, pricesCallTimeout)
	all, err := r.prices.GroupedDaily(callCtx, d)
	cancel()
	if err != nil {
		return 0, 0, fmt.Errorf("grouped daily %s: %w", d, err)
	}
	bars := make([]domain.PriceBar, 0, len(keep))
	for _, b := range all {
		b.Symbol = SECSymbol(b.Symbol)
		if keep[b.Symbol] {
			bars = append(bars, b)
		}
	}
	if len(all) == 0 {
		return 0, 0, nil
	}
	var stored int64
	err = r.store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		if stored, err = storeBars(ctx, tx, bars); err != nil {
			return err
		}
		return tx.MarkGroupedPricesLoaded(ctx, d, len(all))
	})
	return len(all), stored, err
}

// storeBars stores bars, and in the same transaction marks pending the
// companies whose split the bars reach, which a rebuild then applies.
func storeBars(ctx context.Context, tx *store.Store, bars []domain.PriceBar) (int64, error) {
	if err := tx.MarkFundamentalsPendingForSplitsReached(ctx, bars); err != nil {
		return 0, err
	}
	return tx.UpsertPriceBars(ctx, bars)
}

// symbolBars loads the per-symbol daily bars of symbol for the provider's
// whole history window, from its first trading day without a bar.
func (r *Runner) symbolBars(ctx context.Context, symbol string) (int64, error) {
	last := prices.LastFinishedSession(r.now())
	missing, err := r.store.ListMissingPriceDatesForSymbol(ctx, symbol,
		prices.TradingDays(last.AddDays(-providerHistoryDays+1), last))
	if err != nil {
		return 0, err
	}
	if len(missing) == 0 {
		return 0, nil
	}
	return r.symbolBarsFrom(ctx, symbol, missing[0])
}

// symbolBarsFrom loads the per-symbol daily bars of symbol from from to the
// last finished session.
func (r *Runner) symbolBarsFrom(ctx context.Context, symbol string, from domain.Date) (int64, error) {
	last := prices.LastFinishedSession(r.now())
	callCtx, cancel := context.WithTimeout(ctx, pricesCallTimeout)
	bars, err := r.prices.DailyBars(callCtx, ProviderSymbol(symbol), from, last)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("daily bars of %s: %w", symbol, err)
	}
	for i := range bars {
		bars[i].Symbol = symbol
	}
	var stored int64
	err = r.store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		stored, err = storeBars(ctx, tx, bars)
		return err
	})
	return stored, err
}
