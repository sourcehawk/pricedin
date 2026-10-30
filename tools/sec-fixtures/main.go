// Command sec-fixtures downloads the SEC files that the tests in
// server/internal/sec use, trims them, and writes them below -out in the
// layout of the SEC hosts: www.sec.gov/... and data.sec.gov/....
//
// It reads the User-Agent from PRICEDIN_SEC_USER_AGENT. SEC requires a name
// and a contact email address in it.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// companies are the fixture companies: NVDA (fiscal year ends in January,
// splits in 2021 and 2024), AAPL (fiscal year ends in September, split in
// 2020), KO (calendar year, dividends), GRMN (calendar year) and ADM (10-K/A
// and 10-Q/A filings with XBRL facts).
var companies = []string{"NVDA", "AAPL", "KO", "GRMN", "ADM"}

// otherTickers are extra rows for the ticker map.
var otherTickers = []string{"MSFT", "BRK-B", "GOOGL", "GOOG"}

// keptConcepts must stay equal to sec.Concepts() in server/internal/sec. A
// test there fails when a fixture holds a concept outside that set.
var keptConcepts = map[string][]string{
	"us-gaap": {
		"EarningsPerShareDiluted",
		"Revenues",
		"RevenueFromContractWithCustomerExcludingAssessedTax",
		"RevenueFromContractWithCustomerIncludingAssessedTax",
		"SalesRevenueNet",
		"SalesRevenueGoodsNet",
		"OperatingIncomeLoss",
		"NetCashProvidedByUsedInOperatingActivities",
		"NetCashProvidedByUsedInOperatingActivitiesContinuingOperations",
		"PaymentsToAcquirePropertyPlantAndEquipment",
		"PaymentsToAcquireProductiveAssets",
		"WeightedAverageNumberOfDilutedSharesOutstanding",
		"CommonStockDividendsPerShareDeclared",
		"CommonStockDividendsPerShareCashPaid",
		"StockholdersEquityNoteStockSplitConversionRatio1",
		"NetIncomeLoss",
	},
	"dei": {"EntityCommonStockSharesOutstanding"},
}

var keptForms = []string{"10-K", "10-Q", "10-K/A", "10-Q/A"}

func main() {
	out := flag.String("out", "", "testdata/sec folder to write")
	since := flag.String("since", "2020-11-01", "first filing date to keep in filings.recent")
	indexDate := flag.String("index-date", "", "day of the daily master index to keep, YYYY-MM-DD")
	emptyCIK := flag.String("empty-cik", "0000003521", "CIK of a company whose bulk companyfacts entry is {}")
	flag.Parse()
	ua := os.Getenv("PRICEDIN_SEC_USER_AGENT")
	if *out == "" || *indexDate == "" || ua == "" {
		log.Fatal("usage: PRICEDIN_SEC_USER_AGENT='Name email' go run . -out DIR -index-date YYYY-MM-DD")
	}
	day, err := time.Parse(time.DateOnly, *indexDate)
	if err != nil {
		log.Fatalf("-index-date: %v", err)
	}
	f := &fetcher{ua: ua, client: &http.Client{Timeout: time.Minute}}
	g := &generator{out: *out, since: *since, f: f}
	if err := g.run(context.Background(), day, *emptyCIK); err != nil {
		log.Fatal(err)
	}
	log.Printf("done: %d requests", f.requests)
}

type generator struct {
	out   string
	since string
	f     *fetcher
	// files holds the trimmed bodies by output path, for the bulk archives.
	files map[string][]byte
}

func (g *generator) run(ctx context.Context, day time.Time, emptyCIK string) error {
	g.files = map[string][]byte{}
	ciks, err := g.tickers(ctx)
	if err != nil {
		return err
	}
	for _, t := range companies {
		cik := fmt.Sprintf("%010d", ciks[t])
		if err := g.submissions(ctx, cik); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		if err := g.companyFacts(ctx, cik); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	keep := map[int64]bool{}
	for _, t := range companies {
		keep[ciks[t]] = true
	}
	if err := g.dailyIndex(ctx, day, keep); err != nil {
		return err
	}
	return g.archives(ciks, emptyCIK)
}

func (g *generator) write(path string, body []byte) error {
	full := filepath.Join(g.out, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	g.files[path] = body
	log.Printf("write %s (%d bytes)", path, len(body))
	return os.WriteFile(full, body, 0o644)
}

// tickers keeps the rows of the fixture companies, otherTickers, and the
// first row of each exchange value that is not NYSE or Nasdaq.
func (g *generator) tickers(ctx context.Context) (map[string]int64, error) {
	body, err := g.f.get(ctx, "https://www.sec.gov/files/company_tickers_exchange.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Fields []string            `json:"fields"`
		Data   [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode tickers: %w", err)
	}
	want := map[string]bool{}
	for _, t := range append(slices.Clone(companies), otherTickers...) {
		want[t] = true
	}
	ciks := map[string]int64{}
	seenExchange := map[string]bool{"\"NYSE\"": true, "\"Nasdaq\"": true}
	var kept [][]json.RawMessage
	for _, row := range doc.Data {
		var ticker string
		if err := json.Unmarshal(row[2], &ticker); err != nil {
			return nil, err
		}
		exchange := string(row[3])
		if !want[ticker] && seenExchange[exchange] {
			continue
		}
		seenExchange[exchange] = true
		kept = append(kept, row)
		cik, err := strconv.ParseInt(string(row[0]), 10, 64)
		if err != nil {
			return nil, err
		}
		ciks[ticker] = cik
	}
	for _, t := range companies {
		if ciks[t] == 0 {
			return nil, fmt.Errorf("ticker %s not in the SEC ticker map", t)
		}
	}
	doc.Data = kept
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return ciks, g.write("www.sec.gov/files/company_tickers_exchange.json", out)
}

func (g *generator) submissions(ctx context.Context, cik string) error {
	body, err := g.f.get(ctx, "https://data.sec.gov/submissions/CIK"+cik+".json")
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode submissions: %w", err)
	}
	var filings struct {
		Recent map[string][]json.RawMessage `json:"recent"`
		Files  []map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(doc["filings"], &filings); err != nil {
		return fmt.Errorf("decode filings: %w", err)
	}
	if filings.Recent, err = trimColumns(filings.Recent, g.since); err != nil {
		return err
	}
	for _, file := range filings.Files {
		var name string
		if err := json.Unmarshal(file["name"], &name); err != nil {
			return err
		}
		extra, err := g.f.get(ctx, "https://data.sec.gov/submissions/"+name)
		if err != nil {
			return err
		}
		var columns map[string][]json.RawMessage
		if err := json.Unmarshal(extra, &columns); err != nil {
			return fmt.Errorf("decode %s: %w", name, err)
		}
		if columns, err = trimColumns(columns, ""); err != nil {
			return err
		}
		file["filingCount"] = json.RawMessage(strconv.Itoa(len(columns["accessionNumber"])))
		out, err := json.Marshal(columns)
		if err != nil {
			return err
		}
		if err := g.write("data.sec.gov/submissions/"+name, out); err != nil {
			return err
		}
	}
	if doc["filings"], err = json.Marshal(filings); err != nil {
		return err
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return g.write("data.sec.gov/submissions/CIK"+cik+".json", out)
}

// trimColumns keeps the rows of the column arrays whose form is in keptForms
// and whose filing date is on or after since. An empty since keeps every
// date.
func trimColumns(cols map[string][]json.RawMessage, since string) (map[string][]json.RawMessage, error) {
	forms, dates := cols["form"], cols["filingDate"]
	if len(forms) != len(dates) {
		return nil, errors.New("form and filingDate differ in length")
	}
	var rows []int
	for i := range forms {
		var form, date string
		if err := json.Unmarshal(forms[i], &form); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(dates[i], &date); err != nil {
			return nil, err
		}
		if slices.Contains(keptForms, form) && date >= since {
			rows = append(rows, i)
		}
	}
	out := make(map[string][]json.RawMessage, len(cols))
	for key, values := range cols {
		if len(values) != len(forms) {
			return nil, fmt.Errorf("column %s has %d rows, want %d", key, len(values), len(forms))
		}
		kept := make([]json.RawMessage, 0, len(rows))
		for _, i := range rows {
			kept = append(kept, values[i])
		}
		out[key] = kept
	}
	return out, nil
}

func (g *generator) companyFacts(ctx context.Context, cik string) error {
	body, err := g.f.get(ctx, "https://data.sec.gov/api/xbrl/companyfacts/CIK"+cik+".json")
	if err != nil {
		return err
	}
	var doc struct {
		CIK        json.RawMessage                       `json:"cik"`
		EntityName json.RawMessage                       `json:"entityName"`
		Facts      map[string]map[string]json.RawMessage `json:"facts"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode company facts: %w", err)
	}
	facts := map[string]map[string]json.RawMessage{}
	for taxonomy, names := range keptConcepts {
		for _, name := range names {
			if raw, ok := doc.Facts[taxonomy][name]; ok {
				if facts[taxonomy] == nil {
					facts[taxonomy] = map[string]json.RawMessage{}
				}
				facts[taxonomy][name] = raw
			}
		}
	}
	doc.Facts = facts
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return g.write("data.sec.gov/api/xbrl/companyfacts/CIK"+cik+".json", out)
}

// dailyIndex keeps the header of the master index, its first 20 rows, the
// rows of the fixture companies, and up to 5 rows of each kept form.
func (g *generator) dailyIndex(ctx context.Context, day time.Time, fixtureCIKs map[int64]bool) error {
	path := fmt.Sprintf("www.sec.gov/Archives/edgar/daily-index/%d/QTR%d/master.%s.idx",
		day.Year(), (int(day.Month())+2)/3, day.Format("20060102"))
	body, err := g.f.get(ctx, "https://"+path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(body), "\n")
	var out strings.Builder
	inRows, rowCount := false, 0
	perForm := map[string]int{}
	for _, line := range lines {
		if !inRows {
			out.WriteString(line)
			inRows = strings.HasPrefix(line, "-----")
			continue
		}
		fields := strings.Split(strings.TrimRight(line, "\r\n"), "|")
		if len(fields) != 5 {
			continue
		}
		cik, _ := strconv.ParseInt(fields[0], 10, 64)
		form := fields[2]
		keep := rowCount < 20 || fixtureCIKs[cik]
		if slices.Contains(keptForms, form) && perForm[form] < 5 {
			perForm[form]++
			keep = true
		}
		rowCount++
		if keep {
			out.WriteString(line)
		}
	}
	return g.write(path, []byte(out.String()))
}

// archives writes two small archives in the format of the SEC bulk files,
// built from the trimmed fixtures: companyfacts.zip with KO, GRMN and one
// company whose entry is {} (as in the real archive, stored without
// compression), and submissions.zip with KO and NVDA and their extra files.
func (g *generator) archives(ciks map[string]int64, emptyCIK string) error {
	factsEntries := []zipEntry{}
	for _, t := range []string{"KO", "GRMN"} {
		name := fmt.Sprintf("CIK%010d.json", ciks[t])
		factsEntries = append(factsEntries, zipEntry{name, g.files["data.sec.gov/api/xbrl/companyfacts/"+name], zip.Deflate})
	}
	factsEntries = append(factsEntries, zipEntry{"CIK" + emptyCIK + ".json", []byte("{}"), zip.Store})
	if err := g.writeZip("www.sec.gov/Archives/edgar/daily-index/xbrl/companyfacts.zip", factsEntries); err != nil {
		return err
	}
	var subEntries []zipEntry
	for _, t := range []string{"KO", "NVDA"} {
		prefix := fmt.Sprintf("data.sec.gov/submissions/CIK%010d", ciks[t])
		var names []string
		for path := range g.files {
			if strings.HasPrefix(path, prefix) {
				names = append(names, path)
			}
		}
		// Sorted names put each extra file before its main file. The real
		// archive has no fixed order, so the reader must not rely on one.
		slices.Sort(names)
		for _, path := range names {
			subEntries = append(subEntries, zipEntry{filepath.Base(path), g.files[path], zip.Deflate})
		}
	}
	return g.writeZip("www.sec.gov/Archives/edgar/daily-index/bulkdata/submissions.zip", subEntries)
}

type zipEntry struct {
	name   string
	body   []byte
	method uint16
}

func (g *generator) writeZip(path string, entries []zipEntry) error {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		if e.body == nil {
			return fmt.Errorf("%s: no fixture for %s", path, e.name)
		}
		fw, err := w.CreateHeader(&zip.FileHeader{
			Name:     e.name,
			Method:   e.method,
			Modified: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			return err
		}
		if _, err := fw.Write(e.body); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return g.write(path, buf.Bytes())
}

// fetcher sends at most one request every 200 ms, well below the SEC limit
// of 10 a second.
type fetcher struct {
	ua       string
	client   *http.Client
	last     time.Time
	requests int
}

func (f *fetcher) get(ctx context.Context, url string) ([]byte, error) {
	if wait := 200*time.Millisecond - time.Since(f.last); wait > 0 {
		time.Sleep(wait)
	}
	f.last = time.Now()
	f.requests++
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.ua)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	log.Printf("get %s (%d bytes)", url, len(body))
	return body, nil
}
