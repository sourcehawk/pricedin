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
// It reads every version of each source, oldest retrieval day first, and
// on each day in the order of the jobs: the SEC ticker map, submissions,
// company facts and their archives, then the provider's ticker list,
// splits with dividends, grouped daily bars and the bars of one symbol.
// The append-only facts and filings thus keep every version, and the
// newest version of a value that a later load replaces wins, as it did
// for the jobs. A file whose source held the same content on the
// retrieval day before is skipped. Versions of one source from one day
// load in an order that the blob store cannot tell, by hash.
//
// The ticker maps load only from the day of the last sync of their source
// on, because the dated symbol map cannot go back. The grouped daily bars
// are kept for the covered symbols and for every symbol with stored
// splits, dividends or bars of its own, which the jobs fetch for covered
// symbols only. The daily filing indexes, the split-adjusted bars and the
// details of one ticker hold nothing to load.
//
// A file that fails, or a response that lacks a part, does not stop the
// run. Reprocess then returns an error that names the first failures. Like
// every job, it is safe to run again: a second run changes nothing.
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
	return strings.Join(parts, ",")
}

// rawIndex is what a reprocess reads, in order.
type rawIndex struct {
	fetches []*rawFetch
	// extras holds the versions of each extra submissions file by name,
	// oldest first.
	extras map[string][]rawFile
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
	idx, items, err := r.indexRaw(ctx, opts, cikSymbols, &res, &fails)
	if err != nil {
		return res, err
	}
	var keepDay domain.Date
	var keep map[string]bool
	for _, f := range idx.fetches {
		if f.source.kind == rawGrouped && (keep == nil || f.day != keepDay) {
			if keep, err = r.groupedKeep(ctx, idx, cikSymbols); err != nil {
				return res, err
			}
			keepDay = f.day
		}
		err := r.reprocessFetch(ctx, idx, f, opts, keep, &res)
		n := int64(len(f.files))
		switch {
		case err == nil:
			res.Files += n
		case errors.Is(err, errOld):
			res.Old += n
		case errors.Is(err, errIncomplete):
			res.Incomplete += n
			r.logger.InfoContext(ctx, "ingest: reprocess skipped an incomplete response", "source", f.String(), "error", err.Error())
		case ctx.Err() != nil:
			return res, err
		default:
			res.Failed += n
			fails.add(f.String(), err)
			r.logger.WarnContext(ctx, "ingest: reprocess failed to load stored files", "source", f.String(), "error", err.Error())
		}
	}
	pending, err := r.store.ListPendingFundamentals(ctx)
	if err != nil {
		return res, err
	}
	res.Fundamentals, err = r.rebuildMany(ctx, pending)
	return res, errors.Join(fails.err(items), err)
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

// indexRaw lists the stored files that opts selects, as fetches in the
// order of loading, without the fetches whose content repeats the one
// before. It returns the number of items that can fail: the fetches and
// the keys that are not understood.
func (r *Runner) indexRaw(ctx context.Context, opts ReprocessOptions, cikSymbols map[string]bool, res *ReprocessResult, fails *failures) (*rawIndex, int, error) {
	idx := &rawIndex{extras: map[string][]rawFile{}, submitted: map[domain.CIK]bool{}, priceSymbols: map[string]bool{}}
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
	bad := 0
	for _, prefix := range prefixes {
		keys, err := r.blob.List(ctx, prefix)
		if err != nil {
			return nil, 0, fmt.Errorf("ingest: reprocess: %w", err)
		}
		for _, key := range keys {
			file, source, err := r.classify(key, idx)
			if err != nil {
				bad++
				res.Failed++
				fails.add(key, err)
				r.logger.WarnContext(ctx, "ingest: reprocess cannot read a stored file", "key", key, "error", err.Error())
				continue
			}
			if source.kind == rawExtra || (!opts.Since.IsZero() && file.day.Before(opts.Since)) {
				continue
			}
			if source.kind == rawIgnored {
				res.Ignored++
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
		ordered = append(ordered, f)
	}
	slices.SortFunc(ordered, func(a, b *rawFetch) int {
		return cmp.Or(a.day.Time().Compare(b.day.Time()), cmp.Compare(a.source.kind, b.source.kind),
			strings.Compare(a.source.String(), b.source.String()))
	})
	last := map[rawSource]string{}
	for _, f := range ordered {
		sig := f.signature()
		if last[f.source] == sig {
			res.Unchanged += int64(len(f.files))
			continue
		}
		last[f.source] = sig
		idx.fetches = append(idx.fetches, f)
	}
	return idx, len(idx.fetches) + bad, nil
}

// compareRawFiles orders files by day, page and hash.
func compareRawFiles(a, b rawFile) int {
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
			idx.extras[extra] = append(idx.extras[extra], file)
			return file, rawSource{kind: rawExtra}, nil
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

func (r *Runner) reprocessFetch(ctx context.Context, idx *rawIndex, f *rawFetch, opts ReprocessOptions, keep map[string]bool, res *ReprocessResult) error {
	switch f.source.kind {
	case rawSECTickers:
		if err := r.checkSyncDay(ctx, store.TickerSourceSEC, f.day); err != nil {
			return err
		}
		for i := range f.files {
			file := &f.files[i]
			body, err := r.readRaw(ctx, file.key)
			if err != nil {
				return err
			}
			entries, err := sec.ParseTickers(body, f.day)
			if err != nil {
				return err
			}
			got, err := r.storeSECTickers(ctx, entries, f.day)
			res.TickersOpened += got.Opened
			res.TickersClosed += got.Closed
			if err != nil {
				return err
			}
		}
	case rawSubmissions:
		for i := range f.files {
			file := &f.files[i]
			sub, err := r.readSubmissions(ctx, idx, *file)
			if err != nil {
				return err
			}
			got, err := r.storeSECCompany(ctx, sub, nil)
			if err != nil {
				return err
			}
			res.CompaniesChanged += got.Changed
			res.Filings += got.Filings
		}
	case rawFacts:
		for i := range f.files {
			file := &f.files[i]
			body, err := r.readRaw(ctx, file.key)
			if err != nil {
				return err
			}
			facts, err := sec.ParseCompanyFacts(body)
			if err != nil {
				return err
			}
			n, err := r.storeFacts(ctx, f.source.cik, facts)
			if err != nil {
				return err
			}
			res.Facts += n
		}
	case rawArchive:
		for i := range f.files {
			file := &f.files[i]
			if err := r.reprocessArchive(ctx, *file, func(cik domain.CIK) bool {
				return idx.submitted[cik] && (opts.CIK == 0 || cik == opts.CIK)
			}, res); err != nil {
				return err
			}
		}
	default:
		return r.reprocessPriceFetch(ctx, f, opts, keep, res)
	}
	return nil
}

func (r *Runner) reprocessPriceFetch(ctx context.Context, f *rawFetch, opts ReprocessOptions, keep map[string]bool, res *ReprocessResult) error {
	if f.source.kind == rawProviderTickers {
		if err := r.checkSyncDay(ctx, r.stored.Name(), f.day); err != nil {
			return err
		}
	}
	pages, err := r.readPages(ctx, f.files)
	if err != nil {
		return err
	}
	switch f.source.kind {
	case rawProviderTickers:
		got, err := r.storeProviderTickers(ctx, r.stored.Name(), f.day, pages[prices.StoredTickers].Tickers)
		res.TickersOpened += got.Opened
		res.TickersClosed += got.Closed
		return err
	case rawReference:
		splits, okS := pages[prices.StoredSplits]
		dividends, okD := pages[prices.StoredDividends]
		if !okS || !okD {
			return fmt.Errorf("%w: the job stores splits and dividends together, and one of them is missing", errIncomplete)
		}
		got, err := r.storeReference(ctx, SECSymbol(f.source.req.Symbol), splits.Splits, dividends.Dividends)
		res.Splits += got.Splits
		res.Dividends += got.Dividends
		return err
	case rawGrouped:
		all := pages[prices.StoredGroupedDaily].Bars
		mark := opts.CIK == 0
		n, err := r.storeGroupedDay(ctx, f.source.req.Date, all, keep, mark)
		res.Bars += n
		if err == nil && mark && len(all) > 0 {
			res.GroupedDays++
		}
		return err
	case rawBars:
		n, err := r.storeSymbolBars(ctx, SECSymbol(f.source.req.Symbol), pages[prices.StoredDailyBars].Bars)
		res.Bars += n
		return err
	default:
		return fmt.Errorf("ingest: reprocess: unknown source %s", f.source)
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
// each kind of request. It returns errIncomplete when a request lacks a
// page: when a page before the highest one is missing, or no copy of the
// highest one is the last page.
func (r *Runner) readPages(ctx context.Context, files []rawFile) (map[prices.StoredKind]prices.StoredPage, error) {
	type state struct {
		seen map[int]bool
		max  int
		last bool
		page prices.StoredPage
	}
	states := map[prices.StoredKind]*state{}
	for i := range files {
		file := &files[i]
		body, err := r.readRaw(ctx, file.key)
		if err != nil {
			return nil, err
		}
		page, err := r.stored.DecodeStored(file.req, body)
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("%w: %s has %d of its pages", errIncomplete, kind, len(st.seen))
		}
		st.page.Last = true
		out[kind] = st.page
	}
	return out, nil
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
