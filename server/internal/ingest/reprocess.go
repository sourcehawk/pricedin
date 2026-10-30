package ingest

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// The values of ReprocessOptions.Prefix.
const (
	ReprocessSEC    = "sec"
	ReprocessPrices = "prices"
)

// ReprocessOptions narrow a reprocess. The zero value reads every stored
// file.
type ReprocessOptions struct {
	// Prefix is ReprocessSEC or ReprocessPrices to read only the files of
	// SEC or of the price provider, and empty to read both.
	Prefix string
	// Since, when set, leaves out the files retrieved before it.
	Since domain.Date
	// CIK, when set, reads only the files of one company: its submissions
	// and company facts, its entries in the company facts archives, and
	// the splits, dividends and daily bars of the symbols that the
	// database maps to it. The grouped daily files give only the bars of
	// those symbols, and mark no day loaded. The ticker maps are left out.
	CIK domain.CIK
}

// ParseReprocessArgs reads the arguments of the reprocess job:
// --prefix sec|prices, --since YYYY-MM-DD and --cik N.
func ParseReprocessArgs(args []string) (ReprocessOptions, error) {
	fs := flag.NewFlagSet(JobReprocess, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	prefix := fs.String("prefix", "", "")
	since := fs.String("since", "", "")
	cik := fs.String("cik", "", "")
	if err := fs.Parse(args); err != nil {
		return ReprocessOptions{}, fmt.Errorf("ingest: reprocess: %w", err)
	}
	if fs.NArg() > 0 {
		return ReprocessOptions{}, fmt.Errorf("ingest: reprocess: unexpected argument %q", fs.Arg(0))
	}
	var opts ReprocessOptions
	switch *prefix {
	case "", ReprocessSEC, ReprocessPrices:
		opts.Prefix = *prefix
	default:
		return ReprocessOptions{}, fmt.Errorf("ingest: reprocess: --prefix %q, want sec or prices", *prefix)
	}
	var err error
	if *since != "" {
		if opts.Since, err = domain.ParseDate(*since); err != nil {
			return ReprocessOptions{}, fmt.Errorf("ingest: reprocess: --since: %w", err)
		}
	}
	if *cik != "" {
		if opts.CIK, err = domain.ParseCIK(*cik); err != nil {
			return ReprocessOptions{}, fmt.Errorf("ingest: reprocess: --cik: %w", err)
		}
	}
	return opts, nil
}

// ReprocessResult counts what a reprocess read and wrote.
type ReprocessResult struct {
	// Files counts the stored files loaded.
	Files int64
	// Unchanged counts the files left out because their source held the
	// same content on its retrieval day before.
	Unchanged int64
	// Old counts the ticker map files retrieved before the last sync of
	// their source, which the dated symbol map already holds.
	Old int64
	// Incomplete counts the files of a response that the job could not
	// have stored: a page, an extra submissions file, or the dividends
	// that go with the splits, is not stored.
	Incomplete int64
	// Ignored counts the files that hold nothing to load, such as the
	// daily filing indexes.
	Ignored int64
	Failed  int64
	// CompaniesChanged counts the companies whose details a submissions
	// file added or changed.
	CompaniesChanged, Filings, Facts int64
	// TickersOpened and TickersClosed count the rows of the dated symbol
	// maps of SEC and the provider.
	TickersOpened, TickersClosed int64
	// Splits, Dividends and Bars count the provider rows added or changed,
	// and GroupedDays the grouped daily files of a day with bars.
	Splits, Dividends, Bars, GroupedDays int64
	// Fundamentals counts the rebuild of the companies whose inputs
	// changed.
	Fundamentals FundamentalsResult
}

// Stats implements Result.
func (p ReprocessResult) Stats() map[string]int64 {
	return map[string]int64{
		"files": p.Files, "unchanged": p.Unchanged, "old": p.Old, "incomplete": p.Incomplete,
		"ignored": p.Ignored, "failed": p.Failed, "companies_changed": p.CompaniesChanged,
		"filings": p.Filings, "facts": p.Facts, "tickers_opened": p.TickersOpened,
		"tickers_closed": p.TickersClosed, "splits": p.Splits, "dividends": p.Dividends,
		"bars": p.Bars, "grouped_days": p.GroupedDays, "rebuilt": p.Fundamentals.Companies,
		"quarters": p.Fundamentals.Quarters,
	}
}

// Reprocess loads the raw files of the blob store into the database again,
// with the parsers and the store methods of the jobs, and then rebuilds
// the quarters of the companies whose inputs changed. It never sends a
// request to SEC or the price provider. The price files need
// Options.StoredPrices.
//
// The ticker maps load only from the day of the last sync of their source
// on, because the dated symbol map cannot go back. The grouped daily bars
// are kept for the covered symbols and for every symbol with stored
// splits, dividends or bars of its own, which the jobs fetch for covered
// symbols only. The daily filing indexes, the split-adjusted bars and the
// details of one ticker hold nothing to load.
//
// A file that fails, or a response that lacks a part, does not stop the
// run. When a file fails, Reprocess returns an error that names the first
// failures. It is safe to run again: a second run changes nothing, unless
// two sources, such as a grouped day and the bars of one symbol, give one
// bar different values.
func (r *Runner) Reprocess(ctx context.Context, opts ReprocessOptions) (ReprocessResult, error) {
	if opts.Prefix != ReprocessSEC && r.stored == nil {
		return ReprocessResult{}, errors.New("ingest: reprocess of price files needs the reader of stored price responses")
	}
	return runJob(ctx, r, JobReprocess, JobReprocess, false, func(ctx context.Context) (ReprocessResult, error) {
		return r.reprocess(ctx, opts)
	})
}

// rawKind is the kind of a stored source, in the order of the jobs.
type rawKind int

const (
	rawSECTickers rawKind = iota
	rawSubmissions
	rawFacts
	rawArchive
	rawProviderTickers
	rawReference // a symbol's splits and dividends
	rawGrouped
	rawBars
	rawIgnored
	rawExtra // an extra submissions file, which its main file loads
)

// rawFile is one stored file.
type rawFile struct {
	key string
	day domain.Date
	sha string
	// req is the request of a price file.
	req prices.StoredRequest
}

// rawSource names a source whose versions follow each other in time.
type rawSource struct {
	kind rawKind
	cik  domain.CIK
	// req is the request of a price source, without Page. A reference
	// source has the Symbol only.
	req prices.StoredRequest
}

func (s rawSource) String() string {
	switch {
	case s.cik != 0:
		return fmt.Sprintf("%d/%s", s.kind, s.cik)
	case s.req.Symbol != "":
		return fmt.Sprintf("%d/%s/%s_%s", s.kind, s.req.Symbol, s.req.From, s.req.To)
	case !s.req.Date.IsZero():
		return fmt.Sprintf("%d/%s", s.kind, s.req.Date)
	default:
		return fmt.Sprint(s.kind)
	}
}

// rawFetch holds the files of one source retrieved on one day.
type rawFetch struct {
	source rawSource
	day    domain.Date
	files  []rawFile
	// extras identifies the versions of the extra submissions files that
	// a submissions fetch loads with its files.
	extras string
}

func (f *rawFetch) String() string { return f.source.String() + " of " + f.day.String() }

// signature identifies the content of the fetch.
func (f *rawFetch) signature() string {
	parts := make([]string, len(f.files))
	for i := range f.files {
		file := &f.files[i]
		parts[i] = fmt.Sprintf("%s/%d/%s", file.req.Kind, file.req.Page, file.sha)
	}
	slices.Sort(parts)
	return strings.Join(parts, ",") + "|" + f.extras
}

// rawUnit is what a reprocess loads in one step: one fetch, or all the
// fetches of a source whose later versions replace the values of the
// earlier ones. Such a source writes each value once, in its newest form,
// so that a second run writes nothing.
type rawUnit struct {
	source rawSource
	// day is the day of the first fetch of submissions, which other SEC
	// files need first, and the day of the last fetch of a provider
	// source, whose bars other sources can also give.
	day     domain.Date
	fetches []*rawFetch
}

func (u *rawUnit) String() string { return u.source.String() + " from " + u.day.String() }

func (u *rawUnit) files() int64 {
	var n int64
	for _, f := range u.fetches {
		n += int64(len(f.files))
	}
	return n
}

// mergesVersions reports whether the sources of kind load all their
// versions in one step.
func mergesVersions(kind rawKind) bool {
	return kind == rawSubmissions || kind == rawReference || kind == rawGrouped || kind == rawBars
}

// rawIndex is what a reprocess reads, in order.
type rawIndex struct {
	units []*rawUnit
	// keys holds every key listed.
	keys map[string]bool
	// extras holds the versions of each extra submissions file by name,
	// oldest first.
	extras     map[string][]rawFile
	extraNames map[domain.CIK][]string
	// submitted holds the companies with a stored submissions file.
	submitted map[domain.CIK]bool
	// priceSymbols holds the SEC symbols with stored splits, dividends or
	// bars of their own.
	priceSymbols map[string]bool
}

// errIncomplete marks a response that lacks a part, and errOld a ticker
// map older than the last sync of its source.
var (
	errIncomplete = errors.New("ingest: the stored response is incomplete")
	errOld        = errors.New("ingest: the ticker map is older than the last sync")
)

// reprocessPasses bounds the passes over the blob store. A job that runs
// at the same time can store a newer response after a pass listed the
// files, and that pass can then write an older value over the newer one.
// Each later pass loads again the units that have a file that the passes
// before did not list, so the newest version is written last.
const reprocessPasses = 3

func (r *Runner) reprocess(ctx context.Context, opts ReprocessOptions) (ReprocessResult, error) {
	var res ReprocessResult
	var fails failures
	var cikSymbols map[string]bool
	if opts.CIK != 0 {
		records, err := r.store.ListTickers(ctx, opts.CIK)
		if err != nil {
			return res, err
		}
		cikSymbols = map[string]bool{}
		for _, t := range records {
			cikSymbols[t.Symbol] = true
		}
	}
	seen := map[string]bool{}
	items := 0
	for pass := 0; pass < reprocessPasses; pass++ {
		idx, n, err := r.indexRaw(ctx, opts, cikSymbols, seen, &res, &fails)
		if err != nil {
			return res, err
		}
		items += n
		if pass > 0 && len(idx.units) == 0 {
			break
		}
		if err := r.loadUnits(ctx, idx, opts, cikSymbols, &res, &fails); err != nil {
			return res, err
		}
		for k := range idx.keys {
			seen[k] = true
		}
	}
	pending, err := r.store.ListPendingFundamentals(ctx)
	if err != nil {
		return res, err
	}
	res.Fundamentals, err = r.rebuildMany(ctx, pending)
	return res, errors.Join(fails.err(items), err)
}

// loadUnits loads the units of idx in order. It returns an error only
// when the context ended or the database failed outside a unit.
func (r *Runner) loadUnits(ctx context.Context, idx *rawIndex, opts ReprocessOptions, cikSymbols map[string]bool, res *ReprocessResult, fails *failures) error {
	var keepDay domain.Date
	var keep map[string]bool
	for _, u := range idx.units {
		if u.source.kind == rawGrouped && (keep == nil || u.day != keepDay) {
			var err error
			if keep, err = r.groupedKeep(ctx, idx, cikSymbols); err != nil {
				return err
			}
			keepDay = u.day
		}
		out, err := r.loadUnit(ctx, idx, u, opts, keep, res, fails)
		n := u.files()
		switch {
		case err == nil:
			res.Files += n - out.skipped - out.failed
			res.Incomplete += out.skipped
			res.Failed += out.failed
		case errors.Is(err, errOld):
			res.Old += n
		case errors.Is(err, errIncomplete):
			res.Incomplete += n
			r.logger.InfoContext(ctx, "ingest: reprocess skipped an incomplete response", "source", u.String(), "error", err.Error())
		case ctx.Err() != nil:
			return err
		default:
			res.Failed += n
			fails.add(u.String(), err)
			r.logger.WarnContext(ctx, "ingest: reprocess failed to load stored files", "source", u.String(), "error", err.Error())
		}
	}
	return nil
}

// groupedKeep returns the SEC symbols whose grouped daily bars a
// reprocess stores now.
func (r *Runner) groupedKeep(ctx context.Context, idx *rawIndex, cikSymbols map[string]bool) (map[string]bool, error) {
	if cikSymbols != nil {
		return cikSymbols, nil
	}
	members, err := r.covered(ctx)
	if err != nil && !errors.Is(err, ErrEmptyUniverse) {
		return nil, err
	}
	keep := map[string]bool{}
	for s := range idx.priceSymbols {
		keep[s] = true
	}
	for _, m := range members {
		keep[m.Symbol] = true
	}
	return keep, nil
}

// indexRaw lists the stored files that opts selects, as units in the
// order of loading. When seen is not empty, it keeps only the units with a
// file that seen lacks, and counts only such files. It returns the number
// of items that can fail: the units and the keys that are not understood.
func (r *Runner) indexRaw(ctx context.Context, opts ReprocessOptions, cikSymbols, seen map[string]bool, res *ReprocessResult, fails *failures) (*rawIndex, int, error) {
	idx := &rawIndex{
		keys: map[string]bool{}, extras: map[string][]rawFile{}, extraNames: map[domain.CIK][]string{},
		submitted: map[domain.CIK]bool{}, priceSymbols: map[string]bool{},
	}
	var prefixes []string
	if opts.Prefix != ReprocessPrices {
		prefixes = append(prefixes, blob.PrefixSEC)
	}
	if opts.Prefix != ReprocessSEC {
		prefixes = append(prefixes, blob.PrefixPrices+r.stored.Name()+"/")
	}
	type fetchKey struct {
		source rawSource
		day    domain.Date
	}
	fetches := map[fetchKey]*rawFetch{}
	newExtras := map[domain.CIK]bool{}
	bad := 0
	for _, prefix := range prefixes {
		keys, err := r.blob.List(ctx, prefix)
		if err != nil {
			return nil, 0, fmt.Errorf("ingest: reprocess: %w", err)
		}
		for _, key := range keys {
			idx.keys[key] = true
			file, source, err := r.classify(key, idx)
			if err != nil {
				if !seen[key] {
					bad++
					res.Failed++
					fails.add(key, err)
					r.logger.WarnContext(ctx, "ingest: reprocess cannot read a stored file", "key", key, "error", err.Error())
				}
				continue
			}
			if source.kind == rawExtra {
				newExtras[source.cik] = newExtras[source.cik] || !seen[key]
				continue
			}
			if !opts.Since.IsZero() && file.day.Before(opts.Since) {
				continue
			}
			if source.kind == rawIgnored {
				if !seen[key] {
					res.Ignored++
				}
				continue
			}
			if !includeRaw(source, opts.CIK, cikSymbols) {
				continue
			}
			k := fetchKey{source, file.day}
			f := fetches[k]
			if f == nil {
				f = &rawFetch{source: source, day: file.day}
				fetches[k] = f
			}
			f.files = append(f.files, file)
		}
	}
	for _, versions := range idx.extras {
		slices.SortFunc(versions, compareRawFiles)
	}
	ordered := make([]*rawFetch, 0, len(fetches))
	for _, f := range fetches {
		slices.SortFunc(f.files, compareRawFiles)
		if f.source.kind == rawSubmissions {
			f.extras = idx.extraVersions(f.source.cik, f.day)
		}
		ordered = append(ordered, f)
	}
	slices.SortFunc(ordered, compareFetches)
	bySource := map[rawSource][]*rawFetch{}
	var sources []rawSource
	for _, f := range ordered {
		if bySource[f.source] == nil {
			sources = append(sources, f.source)
		}
		bySource[f.source] = append(bySource[f.source], f)
	}
	isNew := func(files []rawFile) int64 {
		var n int64
		for i := range files {
			if !seen[files[i].key] {
				n++
			}
		}
		return n
	}
	for i := range sources {
		source := &sources[i]
		all := bySource[*source]
		var kept []*rawFetch
		for i, f := range all {
			sig := f.signature()
			repeats := i > 0 && all[i-1].signature() == sig
			endsRun := i == len(all)-1 || all[i+1].signature() != sig
			// A ticker map keeps the last of a run of repeats, because the
			// store takes the day of the sync from the last map loaded. A
			// source of bars keeps it too, because its newest response
			// wins over the bars of another source between the repeats.
			keepsLast := source.kind == rawSECTickers || source.kind == rawProviderTickers ||
				source.kind == rawGrouped || source.kind == rawBars
			if repeats && (!keepsLast || !endsRun) {
				res.Unchanged += isNew(f.files)
				continue
			}
			kept = append(kept, f)
		}
		var units []*rawUnit
		if mergesVersions(source.kind) && len(kept) > 0 {
			var day domain.Date
			switch source.kind {
			case rawSubmissions:
				day = kept[0].day
			case rawGrouped, rawBars:
				day = r.lastDayWithBars(ctx, kept)
			default:
				day = kept[len(kept)-1].day
			}
			units = []*rawUnit{{source: *source, day: day, fetches: kept}}
		} else {
			for _, f := range kept {
				units = append(units, &rawUnit{source: *source, day: f.day, fetches: []*rawFetch{f}})
			}
		}
		for _, u := range units {
			fresh := len(seen) == 0 || (source.kind == rawSubmissions && newExtras[source.cik])
			for _, f := range u.fetches {
				fresh = fresh || isNew(f.files) > 0
			}
			if fresh {
				idx.units = append(idx.units, u)
			}
		}
	}
	slices.SortFunc(idx.units, func(a, b *rawUnit) int {
		return cmp.Or(a.day.Time().Compare(b.day.Time()), cmp.Compare(a.source.kind, b.source.kind),
			strings.Compare(a.source.String(), b.source.String()))
	})
	return idx, len(idx.units) + bad, nil
}

// compareFetches orders fetches by day, kind and source.
func compareFetches(a, b *rawFetch) int {
	return cmp.Or(a.day.Time().Compare(b.day.Time()), cmp.Compare(a.source.kind, b.source.kind),
		strings.Compare(a.source.String(), b.source.String()))
}

// extraVersions identifies the versions of the extra submissions files of
// cik that a submissions file of day loads.
func (idx *rawIndex) extraVersions(cik domain.CIK, day domain.Date) string {
	names := slices.Sorted(slices.Values(idx.extraNames[cik]))
	parts := make([]string, 0, len(names))
	for _, name := range slices.Compact(names) {
		if v, ok := nearestVersion(idx.extras[name], day); ok {
			parts = append(parts, name+"/"+v.sha)
		}
	}
	return strings.Join(parts, ",")
}

// compareRawFiles orders files by day, page and hash.
func compareRawFiles(a, b rawFile) int {
	// The blob store keeps no time of day, so two versions of one source
	// from one day are ordered by hash.
	return cmp.Or(a.day.Time().Compare(b.day.Time()), cmp.Compare(a.req.Kind, b.req.Kind),
		cmp.Compare(a.req.Page, b.req.Page), strings.Compare(a.sha, b.sha))
}

// classify returns the file and the source of key. It records in idx the
// extra submissions files, the companies with submissions, and the
// symbols with price files of their own, whatever their retrieval day.
func (r *Runner) classify(key string, idx *rawIndex) (rawFile, rawSource, error) {
	base, day, sha, err := SplitVersionedKey(key)
	if err != nil {
		return rawFile{}, rawSource{}, err
	}
	file := rawFile{key: key, day: day, sha: sha}
	if strings.HasPrefix(base, blob.PrefixSEC) {
		kind, cik, extra, err := secSource(base)
		if err != nil {
			return file, rawSource{}, err
		}
		switch {
		case extra != "":
			if idx.extras[extra] == nil {
				idx.extraNames[cik] = append(idx.extraNames[cik], extra)
			}
			idx.extras[extra] = append(idx.extras[extra], file)
			return file, rawSource{kind: rawExtra, cik: cik}, nil
		case kind == rawSubmissions:
			idx.submitted[cik] = true
		}
		return file, rawSource{kind: kind, cik: cik}, nil
	}
	req, ok, err := r.stored.StoredRequest(base)
	if err != nil {
		return file, rawSource{}, err
	}
	if !ok {
		return file, rawSource{kind: rawIgnored}, nil
	}
	file.req = req
	source := rawSource{}
	switch req.Kind {
	case prices.StoredTickers:
		source.kind = rawProviderTickers
	case prices.StoredSplits, prices.StoredDividends:
		source.kind, source.req = rawReference, prices.StoredRequest{Symbol: req.Symbol}
	case prices.StoredGroupedDaily:
		source.kind, source.req = rawGrouped, prices.StoredRequest{Kind: req.Kind, Date: req.Date}
	case prices.StoredDailyBars:
		source.kind, source.req = rawBars, prices.StoredRequest{Kind: req.Kind, Symbol: req.Symbol, From: req.From, To: req.To}
	default:
		return file, rawSource{}, fmt.Errorf("ingest: stored page of unknown kind %q", req.Kind)
	}
	if req.Symbol != "" {
		idx.priceSymbols[SECSymbol(req.Symbol)] = true
	}
	return file, source, nil
}

// secSource returns the kind of the SEC file with the base key, its
// company, and the name of an extra submissions file.
func secSource(base string) (rawKind, domain.CIK, string, error) {
	dir, name, _ := cutLastSlash(strings.TrimPrefix(base, blob.PrefixSEC))
	switch {
	case dir == "tickers" && name == "company_tickers_exchange.json":
		return rawSECTickers, 0, "", nil
	case dir == "archives" && name == "companyfacts.zip":
		return rawArchive, 0, "", nil
	case dir == "daily-index":
		return rawIgnored, 0, "", nil
	case dir == "submissions" || dir == "companyfacts":
		digits, ok := strings.CutPrefix(name, "CIK")
		if !ok || len(digits) < 10 {
			break
		}
		cik, err := domain.ParseCIK(digits[:10])
		if err != nil {
			break
		}
		switch rest := digits[10:]; {
		case rest == ".json" && dir == "submissions":
			return rawSubmissions, cik, "", nil
		case rest == ".json":
			return rawFacts, cik, "", nil
		case dir == "submissions" && strings.HasPrefix(rest, "-") && strings.HasSuffix(rest, ".json"):
			return rawSubmissions, cik, name, nil
		}
	}
	return 0, 0, "", fmt.Errorf("ingest: %q is not a key of a recorded SEC file", base)
}

// includeRaw reports whether a reprocess for cik, or for every company
// when cik is 0, loads the source.
func includeRaw(s rawSource, cik domain.CIK, cikSymbols map[string]bool) bool {
	if cik == 0 {
		return true
	}
	switch s.kind {
	case rawSubmissions, rawFacts:
		return s.cik == cik
	case rawArchive, rawGrouped:
		return true
	case rawReference, rawBars:
		return cikSymbols[SECSymbol(s.req.Symbol)]
	default:
		return false
	}
}

// unitOutcome counts the files of a unit that did not load while the
// others did.
type unitOutcome struct {
	skipped int64 // incomplete
	failed  int64 // added to fails
}

// versionFailed records a version of a unit that failed. It returns err
// when the context ended, which stops the unit.
func (r *Runner) versionFailed(ctx context.Context, f *rawFetch, files int64, err error, out *unitOutcome, fails *failures) error {
	if ctx.Err() != nil {
		return err
	}
	if errors.Is(err, errIncomplete) {
		out.skipped += files
		r.logger.InfoContext(ctx, "ingest: reprocess skipped an incomplete response", "source", f.String(), "error", err.Error())
		return nil
	}
	out.failed += files
	fails.add(f.String(), err)
	r.logger.WarnContext(ctx, "ingest: reprocess failed to load stored files", "source", f.String(), "error", err.Error())
	return nil
}

// fileFailed records one file that failed to read, parse or load. It
// returns err when the context ended, which stops the unit.
func (r *Runner) fileFailed(ctx context.Context, file *rawFile, err error, out *unitOutcome, fails *failures) error {
	if ctx.Err() != nil {
		return err
	}
	out.failed++
	fails.add(file.key, err)
	r.logger.WarnContext(ctx, "ingest: reprocess failed to load a stored file", "key", file.key, "error", err.Error())
	return nil
}

// lastDayWithBars returns the day of the newest fetch of fetches, oldest
// first, that gives bars, or the day of the first fetch when no later one
// does. A fetch that fails or gives no bar must not move older bars after
// the bars of another source.
func (r *Runner) lastDayWithBars(ctx context.Context, fetches []*rawFetch) domain.Date {
	ignore := func(*rawFile, error) error { return nil }
	for i := len(fetches) - 1; i > 0; i-- {
		pages, _, err := r.readPages(ctx, fetches[i].files, ignore)
		if err == nil && len(pages[prices.StoredGroupedDaily].Bars)+len(pages[prices.StoredDailyBars].Bars) > 0 {
			return fetches[i].day
		}
	}
	return fetches[0].day
}

// loadUnit loads u. A version or a file of u that is incomplete or fails
// does not stop the others; the outcome counts its files.
func (r *Runner) loadUnit(ctx context.Context, idx *rawIndex, u *rawUnit, opts ReprocessOptions, keep map[string]bool, res *ReprocessResult, fails *failures) (unitOutcome, error) {
	switch u.source.kind {
	case rawSubmissions:
		return r.reprocessSubmissions(ctx, idx, u, res, fails)
	case rawReference, rawGrouped, rawBars:
		return r.reprocessPriceUnit(ctx, u, opts, keep, res, fails)
	default:
		var out unitOutcome
		err := r.reprocessFetch(ctx, idx, u.fetches[0], opts, res, &out, fails)
		return out, err
	}
}

func (r *Runner) reprocessFetch(ctx context.Context, idx *rawIndex, f *rawFetch, opts ReprocessOptions, res *ReprocessResult, out *unitOutcome, fails *failures) error {
	switch f.source.kind {
	case rawSECTickers:
		if err := r.checkSyncDay(ctx, store.TickerSourceSEC, f.day); err != nil {
			return err
		}
		for i := range f.files {
			body, err := r.readRaw(ctx, f.files[i].key)
			var entries []sec.TickerEntry
			if err == nil {
				entries, err = sec.ParseTickers(body, f.day)
			}
			if err != nil {
				if err := r.fileFailed(ctx, &f.files[i], err, out, fails); err != nil {
					return err
				}
				continue
			}
			got, err := r.storeSECTickers(ctx, entries, f.day)
			res.TickersOpened += got.Opened
			res.TickersClosed += got.Closed
			if err != nil {
				return err
			}
		}
	case rawFacts:
		for i := range f.files {
			body, err := r.readRaw(ctx, f.files[i].key)
			var facts []domain.Fact
			if err == nil {
				facts, err = sec.ParseCompanyFacts(body)
			}
			if err != nil {
				if err := r.fileFailed(ctx, &f.files[i], err, out, fails); err != nil {
					return err
				}
				continue
			}
			n, err := r.storeFacts(ctx, f.source.cik, facts)
			if err != nil {
				return err
			}
			res.Facts += n
		}
	case rawArchive:
		// sec-bulk stores the archive facts of the covered companies before
		// it asks for their submissions, so a company can have facts and no
		// stored submissions.
		members, err := r.covered(ctx)
		if err != nil && !errors.Is(err, ErrEmptyUniverse) {
			return err
		}
		covered := map[domain.CIK]bool{}
		for _, c := range companiesOf(members) {
			covered[c] = true
		}
		// idx.submitted holds companies of later days too, whose rows do
		// not exist yet.
		stored, err := r.store.ListCompanyCIKs(ctx)
		if err != nil {
			return err
		}
		for i := range f.files {
			if err := r.reprocessArchive(ctx, f.files[i], func(cik domain.CIK) bool {
				_, exists := slices.BinarySearch(stored, cik)
				return exists && (idx.submitted[cik] || covered[cik]) && (opts.CIK == 0 || cik == opts.CIK)
			}, res); err != nil {
				if err := r.fileFailed(ctx, &f.files[i], err, out, fails); err != nil {
					return err
				}
			}
		}
	case rawProviderTickers:
		return r.reprocessProviderTickers(ctx, f, res, out, fails)
	default:
		return fmt.Errorf("ingest: reprocess: unknown source %s", f.source)
	}
	return nil
}

// reprocessSubmissions loads every version of a company's submissions in
// one step: the filings of all versions, and the details of the newest.
func (r *Runner) reprocessSubmissions(ctx context.Context, idx *rawIndex, u *rawUnit, res *ReprocessResult, fails *failures) (unitOutcome, error) {
	var merged sec.Submissions
	var out unitOutcome
	found := false
	for _, f := range u.fetches {
		for i := range f.files {
			sub, err := r.readSubmissions(ctx, idx, f.files[i])
			if err != nil {
				if err := r.versionFailed(ctx, f, 1, err, &out, fails); err != nil {
					return out, err
				}
				continue
			}
			merged.Company = sub.Company
			merged.Filings = append(merged.Filings, sub.Filings...)
			found = true
		}
	}
	if !found {
		return out, nil
	}
	got, err := r.storeSECCompany(ctx, merged, nil)
	res.CompaniesChanged += got.Changed
	res.Filings += got.Filings
	return out, err
}

// reprocessProviderTickers syncs the provider's ticker list of one day.
// Each version of a list of one page is a list of its own. The versions
// of a list of more pages cannot be told apart, so such a day loads only
// when it has one version of each page.
func (r *Runner) reprocessProviderTickers(ctx context.Context, f *rawFetch, res *ReprocessResult, out *unitOutcome, fails *failures) error {
	if err := r.checkSyncDay(ctx, r.stored.Name(), f.day); err != nil {
		return err
	}
	lists := [][]rawFile{f.files}
	pages := map[int]int{}
	for i := range f.files {
		pages[f.files[i].req.Page]++
	}
	switch {
	case len(pages) == 1 && pages[1] > 1:
		lists = nil
		for i := range f.files {
			lists = append(lists, f.files[i:i+1])
		}
	case len(f.files) > len(pages):
		return fmt.Errorf("%w: a page of the ticker list has two versions on one day", errIncomplete)
	}
	failed := func(file *rawFile, err error) error { return r.fileFailed(ctx, file, err, out, fails) }
	for _, list := range lists {
		got, bad, err := r.readPages(ctx, list, failed)
		if errors.Is(err, errIncomplete) {
			out.skipped += int64(len(list)) - bad
			r.logger.InfoContext(ctx, "ingest: reprocess skipped an incomplete response", "source", f.String(), "error", err.Error())
			continue
		}
		if err != nil {
			return err
		}
		changes, err := r.storeProviderTickers(ctx, r.stored.Name(), f.day, got[prices.StoredTickers].Tickers)
		res.TickersOpened += changes.Opened
		res.TickersClosed += changes.Closed
		if err != nil {
			return err
		}
	}
	return nil
}

// reprocessPriceUnit loads every complete version of a provider source in
// one step, and skips the versions that lack a page, or, for a symbol's
// splits, the dividends of the same day. In the one batch, the newest
// version of each value wins.
func (r *Runner) reprocessPriceUnit(ctx context.Context, u *rawUnit, opts ReprocessOptions, keep map[string]bool, res *ReprocessResult, fails *failures) (unitOutcome, error) {
	var out unitOutcome
	var splits []domain.Split
	var dividends []domain.Dividend
	var bars []domain.PriceBar
	// returned is the number of bars of the first grouped version with
	// bars: the jobs mark a day loaded only when the provider returned
	// bars, and the first mark stays.
	returned := 0
	complete := 0
	failed := func(file *rawFile, err error) error { return r.fileFailed(ctx, file, err, &out, fails) }
	for _, f := range u.fetches {
		pages, bad, err := r.readPages(ctx, f.files, failed)
		if err == nil && u.source.kind == rawReference {
			_, okS := pages[prices.StoredSplits]
			_, okD := pages[prices.StoredDividends]
			if !okS || !okD {
				err = fmt.Errorf("%w: the job stores splits and dividends together, and one of them is missing", errIncomplete)
			}
		}
		if err != nil {
			if err := r.versionFailed(ctx, f, int64(len(f.files))-bad, err, &out, fails); err != nil {
				return out, err
			}
			continue
		}
		complete++
		splits = append(splits, pages[prices.StoredSplits].Splits...)
		dividends = append(dividends, pages[prices.StoredDividends].Dividends...)
		day := pages[prices.StoredGroupedDaily].Bars
		if returned == 0 {
			returned = len(day)
		}
		bars = append(bars, day...)
		bars = append(bars, pages[prices.StoredDailyBars].Bars...)
	}
	if complete == 0 {
		return out, nil
	}
	switch u.source.kind {
	case rawReference:
		got, err := r.storeReference(ctx, SECSymbol(u.source.req.Symbol), splits, dividends)
		res.Splits += got.Splits
		res.Dividends += got.Dividends
		return out, err
	case rawGrouped:
		mark := opts.CIK == 0
		n, err := r.storeGroupedDay(ctx, u.source.req.Date, bars, returned, keep, mark)
		res.Bars += n
		if err == nil && mark && returned > 0 {
			res.GroupedDays++
		}
		return out, err
	default:
		n, err := r.storeSymbolBars(ctx, SECSymbol(u.source.req.Symbol), bars)
		res.Bars += n
		return out, err
	}
}

// checkSyncDay returns errOld when source synced its tickers after day.
func (r *Runner) checkSyncDay(ctx context.Context, source string, day domain.Date) error {
	last, err := r.store.GetTickerSyncDate(ctx, source)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	case last.After(day):
		return errOld
	default:
		return nil
	}
}

// readPages decodes the price files of one fetch and joins the pages of
// each kind of request. A file that fails to read or decode goes to
// failed and is left out; readPages returns the number of such files, and
// stops with the error of failed when that is not nil. It returns
// errIncomplete when a request lacks a page: when a page before the
// highest one is missing, or no copy of the highest one is the last page.
func (r *Runner) readPages(ctx context.Context, files []rawFile, failed func(*rawFile, error) error) (map[prices.StoredKind]prices.StoredPage, int64, error) {
	type state struct {
		seen map[int]bool
		max  int
		last bool
		page prices.StoredPage
	}
	states := map[prices.StoredKind]*state{}
	var bad int64
	for i := range files {
		file := &files[i]
		body, err := r.readRaw(ctx, file.key)
		var page prices.StoredPage
		if err == nil {
			page, err = r.stored.DecodeStored(file.req, body)
		}
		if err != nil {
			bad++
			if err := failed(file, err); err != nil {
				return nil, bad, err
			}
			continue
		}
		st := states[file.req.Kind]
		if st == nil {
			st = &state{seen: map[int]bool{}}
			states[file.req.Kind] = st
		}
		st.seen[file.req.Page] = true
		switch {
		case file.req.Page > st.max:
			st.max, st.last = file.req.Page, page.Last
		case file.req.Page == st.max:
			st.last = st.last || page.Last
		}
		st.page.Tickers = append(st.page.Tickers, page.Tickers...)
		st.page.Splits = append(st.page.Splits, page.Splits...)
		st.page.Dividends = append(st.page.Dividends, page.Dividends...)
		st.page.Bars = append(st.page.Bars, page.Bars...)
	}
	out := map[prices.StoredKind]prices.StoredPage{}
	for kind, st := range states {
		if len(st.seen) != st.max || !st.last {
			return nil, bad, fmt.Errorf("%w: %s has %d of its pages", errIncomplete, kind, len(st.seen))
		}
		st.page.Last = true
		out[kind] = st.page
	}
	if len(states) == 0 && bad > 0 {
		return nil, bad, fmt.Errorf("%w: no file of the response could be read", errIncomplete)
	}
	return out, bad, nil
}

// readSubmissions decodes a stored submissions file with the filings of
// the extra files that it lists. For each extra file, it takes the
// version of the same day, else the newest earlier one, else the oldest
// later one.
func (r *Runner) readSubmissions(ctx context.Context, idx *rawIndex, file rawFile) (sec.Submissions, error) {
	body, err := r.readRaw(ctx, file.key)
	if err != nil {
		return sec.Submissions{}, err
	}
	sub, err := sec.ParseSubmissions(body)
	if err != nil {
		return sec.Submissions{}, err
	}
	for _, name := range sub.Files {
		extra, ok := nearestVersion(idx.extras[name], file.day)
		if !ok {
			return sec.Submissions{}, fmt.Errorf("%w: the extra submissions file %s is not stored", errIncomplete, name)
		}
		body, err := r.readRaw(ctx, extra.key)
		if err != nil {
			return sec.Submissions{}, err
		}
		filings, err := sec.ParseSubmissionsFile(sub.Company.CIK, body)
		if err != nil {
			return sec.Submissions{}, fmt.Errorf("%s: %w", name, err)
		}
		sub.Filings = append(sub.Filings, filings...)
	}
	return sub, nil
}

// nearestVersion picks from versions, oldest first, the one of day, else
// the newest earlier one, else the oldest later one.
func nearestVersion(versions []rawFile, day domain.Date) (rawFile, bool) {
	pick := -1
	for i := range versions {
		if versions[i].day.After(day) {
			if pick < 0 {
				return versions[i], true
			}
			break
		}
		pick = i
	}
	if pick < 0 {
		return rawFile{}, false
	}
	return versions[pick], true
}

// reprocessArchive stores the facts of the companies that want accepts
// from a stored company facts archive.
func (r *Runner) reprocessArchive(ctx context.Context, file rawFile, want func(domain.CIK) bool, res *ReprocessResult) error {
	rc, err := r.blob.Get(ctx, file.key)
	if err != nil {
		return fmt.Errorf("ingest: reprocess: %w", err)
	}
	defer func() { _ = rc.Close() }()
	f, err := os.CreateTemp("", "companyfacts-*.zip")
	if err != nil {
		return fmt.Errorf("ingest: reprocess: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	size, err := io.Copy(f, rc)
	if err != nil {
		return fmt.Errorf("ingest: reprocess: read %s: %w", file.key, err)
	}
	err = sec.ReadCompanyFactsArchive(f, size, func(cik domain.CIK, facts []domain.Fact) error {
		if !want(cik) {
			return nil
		}
		n, err := r.storeFacts(ctx, cik, facts)
		res.Facts += n
		return err
	})
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return nil
}

// readRaw returns the stored file under key.
func (r *Runner) readRaw(ctx context.Context, key string) ([]byte, error) {
	rc, err := r.blob.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("ingest: reprocess: %w", err)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("ingest: reprocess: read %s: %w", key, err)
	}
	return body, nil
}
