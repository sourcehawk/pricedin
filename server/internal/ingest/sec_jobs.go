package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// The job names, as ingest_runs and `pricedin jobs run` use them.
const (
	JobSECTickers          = "sec-tickers"
	JobUniverse            = "universe"
	JobSECCompany          = "sec-company"
	JobSECBulk             = "sec-bulk"
	JobSECDaily            = "sec-daily"
	JobPriceReference      = "price-reference"
	JobPricesBackfill      = "prices-backfill"
	JobPricesDaily         = "prices-daily"
	JobFundamentalsRebuild = "fundamentals-rebuild"
	JobCompany             = "company"
	JobDaily               = "daily"
)

// secLock is the advisory lock that every SEC request holds, so that the
// processes that share one SEC contact never send requests at the same
// time.
const secLock = "sec-api"

// secDailyLookback is how far back sec-daily reads the daily index on its
// first run.
const secDailyLookback = 7

// withSEC runs fn under the SEC lock, after the handoff gap, and with the
// time limit of one SEC call.
func (r *Runner) withSEC(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	lk, err := r.locks.wait(ctx, secLock)
	if err != nil {
		return err
	}
	defer r.locks.releaseHandedOff(ctx, lk)
	if err := r.locks.waitForHandoff(ctx, lk); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(ctx)
}

// SECTickersResult counts what sec-tickers wrote.
type SECTickersResult struct {
	Tickers        int64 // rows in the SEC ticker map
	CompaniesAdded int64
	Opened         int64 // new current symbol rows
	Closed         int64
	Updated        int64
}

// Stats implements Result.
func (s SECTickersResult) Stats() map[string]int64 {
	return map[string]int64{
		"tickers": s.Tickers, "companies_added": s.CompaniesAdded,
		"opened": s.Opened, "closed": s.Closed, "updated": s.Updated,
	}
}

// SECTickers loads the SEC ticker map: it adds the companies that are new,
// with their names, and syncs the symbols of the "sec" source as of today.
func (r *Runner) SECTickers(ctx context.Context) (SECTickersResult, error) {
	if err := r.needSEC(JobSECTickers); err != nil {
		return SECTickersResult{}, err
	}
	return runJob(ctx, r, JobSECTickers, JobSECTickers, false, r.syncSECTickers)
}

func (r *Runner) syncSECTickers(ctx context.Context) (SECTickersResult, error) {
	var entries []sec.TickerEntry
	err := r.withSEC(ctx, secCallTimeout, func(ctx context.Context) error {
		var err error
		entries, err = r.sec.Tickers(ctx)
		return err
	})
	if err != nil {
		return SECTickersResult{}, err
	}
	companies := make([]domain.Company, 0, len(entries))
	tickers := make([]domain.Ticker, 0, len(entries))
	for _, e := range entries {
		companies = append(companies, domain.Company{CIK: e.Ticker.CIK, Name: e.Name})
		tickers = append(tickers, e.Ticker)
	}
	res := SECTickersResult{Tickers: int64(len(entries))}
	err = r.store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		if res.CompaniesAdded, err = tx.AddCompanies(ctx, companies); err != nil {
			return err
		}
		changes, err := tx.SyncTickers(ctx, store.TickerSourceSEC, r.today(), tickers)
		res.Opened, res.Closed, res.Updated = changes.Opened, changes.Closed, changes.Updated
		return err
	})
	return res, err
}

// UniverseResult lists the covered universe.
type UniverseResult struct {
	Members []domain.Ticker
}

// Stats implements Result.
func (u UniverseResult) Stats() map[string]int64 {
	return map[string]int64{"members": int64(len(u.Members)), "companies": int64(len(companiesOf(u.Members)))}
}

// Universe returns the covered universe: the current SEC tickers on NYSE
// or Nasdaq, narrowed by PRICEDIN_UNIVERSE. It only reads.
func (r *Runner) Universe(ctx context.Context) (UniverseResult, error) {
	current, err := r.store.ListCurrentTickers(ctx, store.TickerSourceSEC)
	if err != nil {
		return UniverseResult{}, err
	}
	members := SelectUniverse(current, r.narrow)
	if len(r.narrow) > 0 && len(members) < len(r.narrow) {
		found := map[string]bool{}
		for _, m := range members {
			found[m.Symbol] = true
		}
		for _, s := range r.narrow {
			if !found[s] {
				r.logger.WarnContext(ctx, "ingest: PRICEDIN_UNIVERSE names a symbol that is not a current NYSE or Nasdaq ticker", "symbol", s)
			}
		}
	}
	return UniverseResult{Members: members}, nil
}

// covered returns the tickers that the daily jobs keep current: the
// universe, and every current SEC ticker of the companies loaded on demand.
// It returns ErrEmptyUniverse when there are none.
func (r *Runner) covered(ctx context.Context) ([]domain.Ticker, error) {
	u, err := r.Universe(ctx)
	if err != nil {
		return nil, err
	}
	tracked, err := r.store.ListTrackedTickers(ctx)
	if err != nil {
		return nil, err
	}
	out := u.Members
	for _, t := range tracked {
		if !slices.ContainsFunc(out, func(m domain.Ticker) bool { return m.Symbol == t.Symbol }) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, ErrEmptyUniverse
	}
	slices.SortFunc(out, func(a, b domain.Ticker) int { return strings.Compare(a.Symbol, b.Symbol) })
	return out, nil
}

// SECCompanyResult counts what loading companies from SEC wrote.
type SECCompanyResult struct {
	Companies int64 // companies loaded
	NotFound  int64 // companies that SEC has no submissions for
	NoFacts   int64 // companies that SEC has no company facts for
	Changed   int64 // companies whose details changed
	Filings   int64 // filings added
	Facts     int64 // facts added
	Failed    int64
}

// Stats implements Result.
func (s SECCompanyResult) Stats() map[string]int64 {
	return map[string]int64{
		"companies": s.Companies, "not_found": s.NotFound, "no_facts": s.NoFacts, "changed": s.Changed,
		"filings": s.Filings, "facts": s.Facts, "failed": s.Failed,
	}
}

func (s *SECCompanyResult) add(o SECCompanyResult) {
	s.Companies += o.Companies
	s.NotFound += o.NotFound
	s.NoFacts += o.NoFacts
	s.Changed += o.Changed
	s.Filings += o.Filings
	s.Facts += o.Facts
	s.Failed += o.Failed
}

// SECCompany loads the submissions and the company facts of one company,
// named by a ticker or a CIK.
func (r *Runner) SECCompany(ctx context.Context, ref string) (SECCompanyResult, error) {
	if err := r.needSEC(JobSECCompany); err != nil {
		return SECCompanyResult{}, err
	}
	cik, err := r.resolveCIK(ctx, ref)
	if err != nil {
		return SECCompanyResult{}, err
	}
	return runJob(ctx, r, JobSECCompany, JobSECCompany+":"+cik.String(), false, func(ctx context.Context) (SECCompanyResult, error) {
		return r.loadSECCompany(ctx, cik, true)
	})
}

// loadSECCompany fetches the submissions of cik, and its company facts when
// withFacts is true, and stores the company, its filings and its facts. A
// company that SEC does not know counts as NotFound and is no error.
func (r *Runner) loadSECCompany(ctx context.Context, cik domain.CIK, withFacts bool) (SECCompanyResult, error) {
	var sub sec.Submissions
	var facts []domain.Fact
	res := SECCompanyResult{}
	err := r.withSEC(ctx, secCallTimeout, func(ctx context.Context) error {
		var err error
		if sub, err = r.sec.Submissions(ctx, cik); err != nil {
			return err
		}
		if !withFacts {
			return nil
		}
		facts, err = r.sec.CompanyFacts(ctx, cik)
		if errors.Is(err, sec.ErrNotFound) {
			res.NoFacts = 1
			return nil
		}
		return err
	})
	if errors.Is(err, sec.ErrNotFound) {
		r.logger.WarnContext(ctx, "ingest: SEC has no submissions for the company", "cik", cik)
		return SECCompanyResult{NotFound: 1}, nil
	}
	if err != nil {
		return SECCompanyResult{}, fmt.Errorf("company %s: %w", cik, err)
	}
	stored, err := r.storeSECCompany(ctx, sub, facts)
	if err != nil {
		return SECCompanyResult{}, fmt.Errorf("company %s: %w", cik, err)
	}
	stored.NoFacts = res.NoFacts
	return stored, nil
}

// storeSECCompany stores the company before the rows that refer to it, in
// one transaction, and marks its quarters pending when it got new facts.
func (r *Runner) storeSECCompany(ctx context.Context, sub sec.Submissions, facts []domain.Fact) (SECCompanyResult, error) {
	res := SECCompanyResult{Companies: 1}
	err := r.store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		if res.Changed, err = tx.UpsertCompanies(ctx, []domain.Company{sub.Company}); err != nil {
			return err
		}
		if res.Filings, err = tx.InsertFilings(ctx, sub.Filings); err != nil {
			return err
		}
		if res.Facts, err = tx.InsertFacts(ctx, facts); err != nil || res.Facts == 0 {
			return err
		}
		return tx.MarkFundamentalsPending(ctx, []domain.CIK{sub.Company.CIK})
	})
	return res, err
}

// loadSECCompanies loads each company in turn. A company that fails is
// counted and recorded in sec_company_retries, unless the failure stops
// every later one.
func (r *Runner) loadSECCompanies(ctx context.Context, ciks []domain.CIK, withFacts func(domain.CIK) bool) (SECCompanyResult, error) {
	var total SECCompanyResult
	for _, cik := range ciks {
		res, err := r.loadSECCompany(ctx, cik, withFacts(cik))
		if err != nil {
			if fatal(ctx, err) {
				return total, err
			}
			total.Failed++
			r.logger.WarnContext(ctx, "ingest: company failed; a later run tries it again", "cik", cik, "error", err.Error())
			if _, err := r.store.RecordSECRetryFailure(ctx, cik, err.Error(), r.now(), secRetryWait, secRetryMaxWait); err != nil {
				return total, err
			}
			continue
		}
		if err := r.store.DeleteSECRetry(ctx, cik); err != nil {
			return total, err
		}
		total.add(res)
	}
	return total, nil
}

// The retries of a company whose SEC load failed: the first after
// secRetryWait, each later one after twice the wait before, at most
// secRetryMaxWait, and none after secRetryAttempts failures.
const (
	secRetryWait     = 6 * time.Hour
	secRetryMaxWait  = 7 * 24 * time.Hour
	secRetryAttempts = 6
)

// Bulk modes of sec-bulk.
const (
	BulkAuto    = "auto"
	BulkArchive = "archive"
	BulkAPI     = "api"
)

// bulkArchiveMinCompanies is the number of covered companies from which
// sec-bulk in auto mode reads company facts from the bulk archive, which is
// more than 1 GB, instead of one request per company.
const bulkArchiveMinCompanies = 200

// SECBulkResult counts what sec-bulk wrote.
type SECBulkResult struct {
	SECCompanyResult
	ArchiveBytes     int64
	ArchiveCompanies int64 // covered companies whose facts came from the archive
}

// Stats implements Result.
func (s SECBulkResult) Stats() map[string]int64 {
	m := s.SECCompanyResult.Stats()
	m["archive_bytes"], m["archive_companies"] = s.ArchiveBytes, s.ArchiveCompanies
	return m
}

// SECBulk loads every covered company (the universe and the companies
// loaded on demand): the submissions always through the API, and the
// company facts from the bulk archive companyfacts.zip (mode archive),
// through the API (mode api), or from the archive when 200 or more
// companies are covered (mode auto). A company that the archive lacks gets
// its facts through the API. The archive goes to the blob store before it
// is read.
func (r *Runner) SECBulk(ctx context.Context, mode string) (SECBulkResult, error) {
	if err := r.needSEC(JobSECBulk); err != nil {
		return SECBulkResult{}, err
	}
	switch mode {
	case "":
		mode = BulkAuto
	case BulkAuto, BulkArchive, BulkAPI:
	default:
		return SECBulkResult{}, fmt.Errorf("ingest: sec-bulk mode %q, want auto, archive or api", mode)
	}
	return runJob(ctx, r, JobSECBulk, JobSECBulk, false, func(ctx context.Context) (SECBulkResult, error) {
		return r.secBulk(ctx, mode)
	})
}

func (r *Runner) secBulk(ctx context.Context, mode string) (SECBulkResult, error) {
	start := r.today()
	members, err := r.covered(ctx)
	if err != nil {
		return SECBulkResult{}, err
	}
	ciks := companiesOf(members)
	if mode == BulkAuto {
		mode = BulkAPI
		if len(ciks) >= bulkArchiveMinCompanies {
			mode = BulkArchive
		}
	}
	var res SECBulkResult
	fromArchive := map[domain.CIK]bool{}
	if mode == BulkArchive {
		if res.ArchiveBytes, err = r.loadFactsArchive(ctx, ciks, &res, fromArchive); err != nil {
			return res, err
		}
	}
	loaded, err := r.loadSECCompanies(ctx, ciks, func(cik domain.CIK) bool { return !fromArchive[cik] })
	res.add(loaded)
	if err != nil {
		return res, err
	}
	// sec-daily then starts from the first day that the bulk load may have
	// missed, not a week back.
	_, err = r.store.SetIngestCursorIfMissing(ctx, JobSECDaily, start)
	return res, err
}

// loadFactsArchive downloads companyfacts.zip to a temporary file, records
// it in the blob store, and stores the facts of the companies of ciks. It
// marks in fromArchive each company that the archive holds.
func (r *Runner) loadFactsArchive(ctx context.Context, ciks []domain.CIK, res *SECBulkResult, fromArchive map[domain.CIK]bool) (int64, error) {
	f, err := os.CreateTemp("", "companyfacts-*.zip")
	if err != nil {
		return 0, fmt.Errorf("ingest: archive: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()

	hash := sha256.New()
	dlCtx, cancel := context.WithTimeout(ctx, archiveTimeout)
	size, err := r.sec.DownloadCompanyFactsArchive(dlCtx, io.MultiWriter(f, hash))
	cancel()
	if err != nil {
		return 0, fmt.Errorf("ingest: download company facts archive: %w", err)
	}
	key, err := versionedKey(blob.PrefixSEC+"archives/companyfacts.zip", r.today(), hex.EncodeToString(hash.Sum(nil))[:16])
	if err != nil {
		return 0, err
	}
	err = putOnce(ctx, r.blob, key, func() error {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return r.blob.Put(ctx, key, f)
	})
	if err != nil {
		return 0, err
	}

	want := map[domain.CIK]bool{}
	for _, c := range ciks {
		want[c] = true
	}
	err = sec.ReadCompanyFactsArchive(f, size, func(cik domain.CIK, facts []domain.Fact) error {
		if !want[cik] {
			return nil
		}
		var n int64
		err := r.store.WithTx(ctx, func(tx *store.Store) error {
			var err error
			if n, err = tx.InsertFacts(ctx, facts); err != nil || n == 0 {
				return err
			}
			return tx.MarkFundamentalsPending(ctx, []domain.CIK{cik})
		})
		if err != nil {
			return err
		}
		fromArchive[cik] = true
		res.ArchiveCompanies++
		res.Facts += n
		return nil
	})
	if err != nil {
		return size, fmt.Errorf("ingest: %w", err)
	}
	return size, nil
}

// SECDailyResult counts what sec-daily read and wrote.
type SECDailyResult struct {
	SECCompanyResult
	Indexes int64 // daily indexes read
	Missing int64 // weekdays without an index, such as holidays
	Filers  int64 // covered companies that filed a periodic report
	// RetriesDue counts the companies tried again after a failure,
	// RetriesWaiting those that wait for a later try, and
	// RetriesExhausted those that failed too often to be tried again.
	RetriesDue, RetriesWaiting, RetriesExhausted int64
}

// Stats implements Result.
func (s SECDailyResult) Stats() map[string]int64 {
	m := s.SECCompanyResult.Stats()
	m["indexes"], m["missing"], m["filers"] = s.Indexes, s.Missing, s.Filers
	m["retries_due"], m["retries_waiting"], m["retries_exhausted"] = s.RetriesDue, s.RetriesWaiting, s.RetriesExhausted
	return m
}

// SECDaily reads the daily filing index of each weekday from the last
// index it read, or from the day sec-bulk started, or a week back when
// neither exists, to today, and loads every covered company that filed a
// 10-Q, a 10-K or a 10-KT, or an amendment of one, and every company whose
// earlier load failed and is due for another try. A day without an index,
// such as a holiday or a day that SEC has not finished, is skipped; the
// next run reads the last index again and every day after it, so a late
// index is never missed. A company that fails does not fail the run,
// unless SEC refused the client or the context ended: it is tried again in
// a later run, up to secRetryAttempts failures.
func (r *Runner) SECDaily(ctx context.Context) (SECDailyResult, error) {
	if err := r.needSEC(JobSECDaily); err != nil {
		return SECDailyResult{}, err
	}
	return runJob(ctx, r, JobSECDaily, JobSECDaily, false, r.secDaily)
}

func (r *Runner) secDaily(ctx context.Context) (SECDailyResult, error) {
	members, err := r.covered(ctx)
	if err != nil {
		return SECDailyResult{}, err
	}
	covered := map[domain.CIK]bool{}
	for _, c := range companiesOf(members) {
		covered[c] = true
	}
	today := r.today()
	from, err := r.store.GetIngestCursor(ctx, JobSECDaily)
	if errors.Is(err, store.ErrNotFound) {
		from, err = today.AddDays(-secDailyLookback), nil
	}
	if err != nil {
		return SECDailyResult{}, err
	}

	var res SECDailyResult
	var lastRead domain.Date
	filed := map[domain.CIK]bool{}
	for d := from; !d.After(today); d = d.AddDays(1) {
		if wd := d.Time().Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		var filings []domain.Filing
		err := r.withSEC(ctx, secCallTimeout, func(ctx context.Context) error {
			var err error
			filings, err = r.sec.DailyIndex(ctx, d)
			return err
		})
		if errors.Is(err, sec.ErrNotFound) {
			res.Missing++
			continue
		}
		if err != nil {
			return res, fmt.Errorf("daily index %s: %w", d, err)
		}
		res.Indexes++
		lastRead = d
		for _, f := range filings {
			if covered[f.CIK] && isPeriodicReport(f.Form) {
				filed[f.CIK] = true
			}
		}
	}
	res.Filers = int64(len(filed))
	// The cursor moves on also when a company fails: the retries keep it.
	if !lastRead.IsZero() {
		if err := r.store.SetIngestCursor(ctx, JobSECDaily, lastRead); err != nil {
			return res, err
		}
	}
	due, err := r.store.ListDueSECRetries(ctx, r.now(), secRetryAttempts)
	if err != nil {
		return res, err
	}
	res.RetriesDue = int64(len(due))
	for _, c := range due {
		filed[c] = true
	}
	loaded, err := r.loadSECCompanies(ctx, slices.Sorted(maps.Keys(filed)), func(domain.CIK) bool { return true })
	res.add(loaded)
	if err != nil {
		return res, err
	}
	res.RetriesWaiting, res.RetriesExhausted, err = r.store.CountSECRetries(ctx, secRetryAttempts)
	return res, err
}
