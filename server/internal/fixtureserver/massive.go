package fixtureserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	// The bar timestamps are midnight in New York, and the image has no
	// time zone files.
	_ "time/tzdata"
)

// bar is one result of an aggregates response, in the field order of the
// fixture files.
type bar struct {
	Ticker string  `json:"T,omitempty"`
	V      float64 `json:"v"`
	VW     float64 `json:"vw"`
	O      float64 `json:"o"`
	C      float64 `json:"c"`
	H      float64 `json:"h"`
	L      float64 `json:"l"`
	T      int64   `json:"t"`
	N      int64   `json:"n"`
	date   string
}

type split struct {
	Date string  `json:"execution_date"`
	From float64 `json:"split_from"`
	To   float64 `json:"split_to"`
}

type massive struct {
	dir string
	// aggs holds the bars of each symbol, oldest first.
	aggs    map[string][]bar
	symbols []string
	// first is the earliest date of any bar. Like the plan's history
	// window, nothing before it is available.
	first string
}

var (
	symbolRE = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]*$`)
	cursorRE = regexp.MustCompile(`^page-\d+$`)
)

func newMassive(dir string) (*massive, error) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("fixtureserver: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "aggs", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("fixtureserver: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("fixtureserver: no aggregates files in %s", dir)
	}
	m := &massive{dir: dir, aggs: map[string][]bar{}}
	for _, f := range files {
		body, err := os.ReadFile(f) //nolint:gosec // the files come from the configured fixture folder
		if err != nil {
			return nil, fmt.Errorf("fixtureserver: %w", err)
		}
		var doc struct {
			Results []bar `json:"results"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("fixtureserver: %s: %w", f, err)
		}
		symbol := strings.TrimSuffix(filepath.Base(f), ".json")
		for i := range doc.Results {
			b := &doc.Results[i]
			b.date = time.UnixMilli(b.T).In(newYork).Format(time.DateOnly)
			if m.first == "" || b.date < m.first {
				m.first = b.date
			}
		}
		m.aggs[symbol] = doc.Results
		m.symbols = append(m.symbols, symbol)
	}
	slices.Sort(m.symbols)
	return m, nil
}

func (m *massive) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/aggs/grouped/locale/us/market/stocks/{date}", m.grouped)
	mux.HandleFunc("GET /v2/aggs/ticker/{symbol}/range/{multiplier}/{timespan}/{from}/{to}", m.dailyBars)
	mux.HandleFunc("GET /stocks/v1/splits", m.events("splits"))
	mux.HandleFunc("GET /stocks/v1/dividends", m.events("dividends"))
	mux.HandleFunc("GET /v3/reference/tickers", m.tickers)
	mux.HandleFunc("GET /v3/reference/tickers/{symbol}", m.ticker)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusNotFound, "NOT_FOUND", "no such endpoint")
	})
	return mux
}

// requireBearer answers 401 unless the request carries the bearer token.
func requireBearer(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case auth == "":
			writeStatus(w, http.StatusUnauthorized, "ERROR", "API Key was not provided")
		case auth != "Bearer "+key:
			writeStatus(w, http.StatusUnauthorized, "ERROR", "Unknown API Key")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// grouped serves grouped/<date>.json when it exists. For another date it
// builds the response from the bar of that date in every aggregates file,
// which gives an empty result on a day without trading.
func (m *massive) grouped(w http.ResponseWriter, r *http.Request) {
	date := r.PathValue("date")
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		writeStatus(w, http.StatusBadRequest, "ERROR", "invalid date")
		return
	}
	if date < m.first {
		m.notAuthorized(w)
		return
	}
	if body, err := os.ReadFile(filepath.Join(m.dir, "grouped", date+".json")); err == nil { //nolint:gosec // the date was checked above
		writeJSONBody(w, body)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "read fixture", http.StatusInternalServerError)
		return
	}
	var results []bar
	for _, symbol := range m.symbols {
		bars := m.aggs[symbol]
		i, found := slices.BinarySearchFunc(bars, date, func(b bar, d string) int { return strings.Compare(b.date, d) })
		if found {
			b := bars[i]
			b.Ticker = symbol
			results = append(results, b)
		}
	}
	writeBars(w, "", results, false)
}

// dailyBars cuts the aggregates file of the symbol to the requested range.
// An unknown symbol gives an empty result, as Massive answers.
func (m *massive) dailyBars(w http.ResponseWriter, r *http.Request) {
	symbol, from, to := r.PathValue("symbol"), r.PathValue("from"), r.PathValue("to")
	_, errFrom := time.Parse(time.DateOnly, from)
	_, errTo := time.Parse(time.DateOnly, to)
	if r.PathValue("multiplier") != "1" || r.PathValue("timespan") != "day" || errFrom != nil || errTo != nil || from > to {
		writeStatus(w, http.StatusBadRequest, "ERROR", "the fixture server answers daily ranges only")
		return
	}
	if to < m.first {
		m.notAuthorized(w)
		return
	}
	adjusted := r.URL.Query().Get("adjusted") == "true"
	var results []bar
	for _, b := range m.aggs[symbol] {
		if b.date >= from && b.date <= to {
			results = append(results, b)
		}
	}
	if adjusted && len(results) > 0 {
		splits, err := m.splits(symbol)
		if err != nil {
			http.Error(w, "read fixture", http.StatusInternalServerError)
			return
		}
		for i := range results {
			adjust(&results[i], splits)
		}
	}
	head := fmt.Sprintf(`"ticker":%q,`, symbol)
	writeBars(w, head, results, adjusted)
}

// adjust divides the prices of b by the ratios of the splits after its date,
// and multiplies its volume.
func adjust(b *bar, splits []split) {
	f := 1.0
	for _, s := range splits {
		if s.Date > b.date {
			f *= s.To / s.From
		}
	}
	b.O, b.H, b.L, b.C, b.VW = b.O/f, b.H/f, b.L/f, b.C/f, b.VW/f
	b.V *= f
}

func (m *massive) splits(symbol string) ([]split, error) {
	body, err := os.ReadFile(filepath.Join(m.dir, "splits", symbol+".json")) //nolint:gosec // the symbol is one of the aggregates files
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Results []split `json:"results"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	return doc.Results, nil
}

// events serves splits/<ticker>.json or dividends/<ticker>.json, and an
// empty result for a ticker without a file.
func (m *massive) events(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query().Get("ticker")
		if !symbolRE.MatchString(symbol) {
			writeStatus(w, http.StatusBadRequest, "ERROR", "want a ticker")
			return
		}
		m.serveFile(w, filepath.Join(kind, symbol+".json"), func() {
			writeJSONBody(w, fmt.Appendf(nil, `{"status":"OK","request_id":%q,"results":[]}`, randomRequestID()))
		})
	}
}

// tickers serves tickers/page-<N>.json, where the cursor parameter names
// the page; the first page has no cursor.
func (m *massive) tickers(w http.ResponseWriter, r *http.Request) {
	page := "page-1"
	if c := r.URL.Query().Get("cursor"); c != "" {
		page = c
	}
	if !cursorRE.MatchString(page) {
		writeStatus(w, http.StatusBadRequest, "ERROR", "invalid cursor")
		return
	}
	m.serveFile(w, filepath.Join("tickers", page+".json"), func() {
		writeStatus(w, http.StatusBadRequest, "ERROR", "invalid cursor")
	})
}

// ticker serves ticker/<SYMBOL>.json, and 404 NOT_FOUND for another
// symbol.
func (m *massive) ticker(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if !symbolRE.MatchString(symbol) {
		writeStatus(w, http.StatusNotFound, "NOT_FOUND", "Ticker not found.")
		return
	}
	m.serveFile(w, filepath.Join("ticker", symbol+".json"), func() {
		writeStatus(w, http.StatusNotFound, "NOT_FOUND", "Ticker not found.")
	})
}

// serveFile writes the fixture file rel, or calls missing when it does not
// exist.
func (m *massive) serveFile(w http.ResponseWriter, rel string, missing func()) {
	body, err := os.ReadFile(filepath.Join(m.dir, rel)) //nolint:gosec // callers check the variable part of rel
	switch {
	case errors.Is(err, fs.ErrNotExist):
		missing()
	case err != nil:
		http.Error(w, "read fixture", http.StatusInternalServerError)
	default:
		writeJSONBody(w, body)
	}
}

func (m *massive) notAuthorized(w http.ResponseWriter) {
	writeStatus(w, http.StatusForbidden, "NOT_AUTHORIZED",
		"Your plan doesn't include this data timeframe. Please upgrade your plan.")
}

// writeBars writes an aggregates response. head holds the fields before
// queryCount, such as the ticker of a per-symbol response.
func writeBars(w http.ResponseWriter, head string, results []bar, adjusted bool) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `{%s"queryCount":%d,"resultsCount":%d,"adjusted":%t,`, head, len(results), len(results), adjusted)
	if len(results) > 0 {
		lines, _ := json.Marshal(results) //nolint:errchkjson // bars hold only finite numbers and strings
		fmt.Fprintf(&b, `"results":%s,`, lines)
	}
	fmt.Fprintf(&b, `"status":"OK","request_id":%q,"count":%d}`, randomRequestID(), len(results))
	writeJSONBody(w, b.Bytes())
}

func writeStatus(w http.ResponseWriter, code int, status, message string) {
	body, _ := json.Marshal(map[string]string{ //nolint:errchkjson // a map of strings always marshals
		"status": status, "request_id": randomRequestID(), "message": message,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// writeJSONBody writes body with a new random request_id in place of its
// own, as Massive gives every response a request ID of its own.
func writeJSONBody(w http.ResponseWriter, body []byte) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) == nil {
		if _, ok := fields["request_id"]; ok {
			fields["request_id"], _ = json.Marshal(randomRequestID()) //nolint:errchkjson // a string always marshals
			if out, err := json.Marshal(fields); err == nil {
				body = out
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body) //nolint:gosec // fixture files and JSON documents, served as JSON
}
