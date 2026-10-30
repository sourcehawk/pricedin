package ingest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// FundamentalsResult counts what fundamentals-rebuild wrote.
type FundamentalsResult struct {
	Companies int64
	Quarters  int64
	Values    int64
	Conflicts int64
	SECSplits int64 // SEC-confirmed splits added
	Failed    int64
}

// Stats implements Result.
func (f FundamentalsResult) Stats() map[string]int64 {
	return map[string]int64{
		"companies": f.Companies, "quarters": f.Quarters, "values": f.Values,
		"conflicts": f.Conflicts, "sec_splits": f.SECSplits, "failed": f.Failed,
	}
}

func (f *FundamentalsResult) add(o FundamentalsResult) {
	f.Companies += o.Companies
	f.Quarters += o.Quarters
	f.Values += o.Values
	f.Conflicts += o.Conflicts
	f.SECSplits += o.SECSplits
	f.Failed += o.Failed
}

// FundamentalsRebuild derives quarters again from stored facts and provider
// splits, and replaces the stored quarters and conflicts. ref is a ticker,
// a CIK, "all" for every company with facts, or "pending" for the companies
// whose quarters are marked pending.
func (r *Runner) FundamentalsRebuild(ctx context.Context, ref string) (FundamentalsResult, error) {
	if strings.EqualFold(ref, "pending") {
		return r.rebuildPending(ctx)
	}
	if strings.EqualFold(ref, "all") {
		return runJob(ctx, r, JobFundamentalsRebuild, JobFundamentalsRebuild+":all", false, func(ctx context.Context) (FundamentalsResult, error) {
			latest, err := r.store.ListLatestFiled(ctx)
			if err != nil {
				return FundamentalsResult{}, err
			}
			return r.rebuildMany(ctx, slices.Sorted(maps.Keys(latest)))
		})
	}
	cik, err := r.resolveCIK(ctx, ref)
	if err != nil {
		return FundamentalsResult{}, err
	}
	return runJob(ctx, r, JobFundamentalsRebuild, JobFundamentalsRebuild+":"+cik.String(), false, func(ctx context.Context) (FundamentalsResult, error) {
		return r.rebuild(ctx, cik)
	})
}

// A mark stays until a rebuild succeeds, so a company whose rebuild failed
// or did not run is rebuilt by a later run.
func (r *Runner) rebuildPending(ctx context.Context) (FundamentalsResult, error) {
	return runJob(ctx, r, JobFundamentalsRebuild, JobFundamentalsRebuild+":pending", false, func(ctx context.Context) (FundamentalsResult, error) {
		ciks, err := r.store.ListPendingFundamentals(ctx)
		if err != nil {
			return FundamentalsResult{}, err
		}
		return r.rebuildMany(ctx, ciks)
	})
}

func (r *Runner) rebuildMany(ctx context.Context, ciks []domain.CIK) (FundamentalsResult, error) {
	var total FundamentalsResult
	var fails failures
	for _, cik := range ciks {
		res, err := r.rebuild(ctx, cik)
		if err != nil {
			if ctx.Err() != nil {
				return total, err
			}
			total.Failed++
			fails.add(cik.String(), err)
			r.logger.WarnContext(ctx, "ingest: fundamentals rebuild failed", "cik", cik, "error", err.Error())
			continue
		}
		total.add(res)
	}
	return total, fails.err(len(ciks))
}

// rebuild derives and stores the quarters of one company in one
// transaction. Rebuilds of one company may run at the same time: the last
// to commit read the newest inputs. When it commits, it clears the
// company's pending mark, unless the mark was set again after it read the
// mark.
func (r *Runner) rebuild(ctx context.Context, cik domain.CIK) (FundamentalsResult, error) {
	var res FundamentalsResult
	err := r.store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.LockCompanyFundamentals(ctx, cik); err != nil {
			return err
		}
		version, pending, err := tx.GetFundamentalsPending(ctx, cik)
		if err != nil {
			return err
		}
		facts, err := tx.ListFacts(ctx, cik, store.FactQuery{})
		if err != nil {
			return err
		}
		splits, checked, err := r.providerSplits(ctx, tx, cik)
		if err != nil {
			return err
		}
		series, err := fundamentals.Derive(cik, facts, fundamentals.Options{Splits: splits, ProviderChecked: checked})
		if err != nil {
			return fmt.Errorf("derive %s: %w", cik, err)
		}
		values := quarterlyValues(series)
		conflicts := storeConflicts(series.Conflicts)
		var secSplits []domain.Split
		for _, s := range series.Splits {
			if s.Source == store.TickerSourceSEC {
				s.CIK = cik
				secSplits = append(secSplits, s)
			}
		}
		res = FundamentalsResult{
			Companies: 1, Quarters: int64(len(series.Quarters)),
			Values: int64(len(values)), Conflicts: int64(len(conflicts)),
		}
		if err := tx.ReplaceQuarterlyFundamentals(ctx, cik, values); err != nil {
			return err
		}
		if err := tx.ReplaceFundamentalConflicts(ctx, cik, conflicts); err != nil {
			return err
		}
		if res.SECSplits, err = tx.InsertStockSplits(ctx, secSplits); err != nil {
			return err
		}
		if !pending {
			return nil
		}
		return tx.ClearFundamentalsPending(ctx, cik, version)
	})
	if err != nil {
		return FundamentalsResult{}, err
	}
	return res, nil
}

// providerSplits returns the provider's split events of the company's
// current SEC symbols that are dated on or before the symbol's newest bar,
// and none for a symbol without bars. It also reports whether any symbol
// was checked with the provider. Share classes split together, so an event
// that two symbols share counts once.
func (r *Runner) providerSplits(ctx context.Context, tx *store.Store, cik domain.CIK) ([]domain.Split, bool, error) {
	records, err := tx.ListTickers(ctx, cik)
	if err != nil {
		return nil, false, err
	}
	checked := false
	byDate := map[domain.Date]domain.Split{}
	for _, t := range records {
		if t.Source != store.TickerSourceSEC || t.ValidTo != nil {
			continue
		}
		ok, err := tx.IsPriceReferenceChecked(ctx, t.Symbol)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		checked = true
		splits, err := tx.ListPriceSplits(ctx, t.Symbol)
		if err != nil {
			return nil, false, err
		}
		// The view prices_adjusted applies a split only once the symbol has
		// a bar on or after the split's date.
		basis, err := tx.GetLatestPriceDate(ctx, t.Symbol)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		for _, s := range splits {
			if s.Date.After(basis) {
				continue
			}
			if _, dup := byDate[s.Date]; !dup {
				byDate[s.Date] = s
			}
		}
	}
	out := slices.SortedFunc(maps.Values(byDate), func(a, b domain.Split) int { return a.Date.Time().Compare(b.Date.Time()) })
	return out, checked, nil
}

// quarterlyValues flattens a series into stored values, quarter by
// quarter in the order of fundamentals.Metrics.
func quarterlyValues(s fundamentals.Series) []store.QuarterlyValue {
	var out []store.QuarterlyValue
	for _, q := range s.Quarters {
		for _, m := range fundamentals.Metrics() {
			v, ok := q.Values[m]
			if !ok {
				continue
			}
			out = append(out, store.QuarterlyValue{
				FiscalYear: q.FiscalYear, FiscalQuarter: q.FiscalQuarter, Label: q.Label,
				Metric: string(m), Value: v.V, Method: string(v.Method), Estimate: v.Estimate,
				PeriodStart: q.PeriodStart, PeriodEnd: q.PeriodEnd,
				Filed: v.Filed, FirstFiled: v.FirstFiled, Inputs: sourceRefs(v.Inputs),
			})
		}
	}
	return out
}

func sourceRefs(in []fundamentals.SourceRef) []store.SourceRef {
	out := make([]store.SourceRef, len(in))
	for i := range in {
		out[i] = sourceRef(in[i])
	}
	return out
}

func sourceRef(ref fundamentals.SourceRef) store.SourceRef {
	return store.SourceRef{
		Accession: ref.Accession, Form: ref.Form, Filed: ref.Filed, Concept: ref.Concept,
		PeriodStart: ref.PeriodStart, PeriodEnd: ref.PeriodEnd, Value: ref.Value,
		SplitRatio: ref.SplitRatio, Scale: ref.Scale,
	}
}

// optionalRef returns nil for a zero SourceRef, which a conflict uses for
// a filing it does not have.
func optionalRef(ref fundamentals.SourceRef) *store.SourceRef {
	if ref.Accession == "" && ref.Concept == "" {
		return nil
	}
	out := sourceRef(ref)
	return &out
}

func storeConflicts(in []fundamentals.Conflict) []store.FundamentalConflict {
	out := make([]store.FundamentalConflict, len(in))
	for i := range in {
		c := &in[i]
		out[i] = store.FundamentalConflict{
			Kind: string(c.Kind), Concept: c.Concept, PeriodStart: c.PeriodStart, PeriodEnd: c.PeriodEnd,
			Original: optionalRef(c.Original), Restated: optionalRef(c.Restated),
			Observed: c.Observed, Expected: c.Expected,
		}
		if c.Split != nil {
			out[i].Split = &store.ConflictSplit{
				Date: c.Split.Date, From: c.Split.From, To: c.Split.To,
				Source: c.Split.Source, Evidence: c.Split.Evidence,
			}
		}
	}
	return out
}
