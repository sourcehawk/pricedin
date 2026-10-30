package sec_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

const (
	companyFactsZip = "www.sec.gov/Archives/edgar/daily-index/xbrl/companyfacts.zip"
	submissionsZip  = "www.sec.gov/Archives/edgar/daily-index/bulkdata/submissions.zip"
)

func buildZip(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestReadCompanyFactsArchiveCallsOnceForEachCompany(t *testing.T) {
	body := readFixture(t, companyFactsZip)
	got := map[domain.CIK][]domain.Fact{}
	err := sec.ReadCompanyFactsArchive(bytes.NewReader(body), int64(len(body)), func(cik domain.CIK, facts []domain.Fact) error {
		if _, seen := got[cik]; seen {
			t.Errorf("CIK %s twice", cik)
		}
		got[cik] = facts
		return nil
	})
	if err != nil {
		t.Fatalf("ReadCompanyFactsArchive() error = %v", err)
	}
	for _, cik := range []domain.CIK{cikKO, cikGRMN} {
		want := companyFactsFixture(t, cik)
		if diff := cmp.Diff(want, got[cik]); diff != "" {
			t.Errorf("facts of %s differ from the parsed fixture (-want +got):\n%s", cik, diff)
		}
	}
	// The real archive holds entries of "{}" for companies without facts.
	facts, ok := got[3521]
	if !ok || len(facts) != 0 {
		t.Errorf("empty entry: called = %v, facts = %d, want a call with no facts", ok, len(facts))
	}
	if len(got) != 3 {
		t.Errorf("calls = %d, want 3", len(got))
	}
}

// readLog records each read through it, with the number of callbacks that
// had returned when the read happened.
type readLog struct {
	r       io.ReaderAt
	mu      sync.Mutex
	reads   []loggedRead
	returns int
}

type loggedRead struct {
	from, to int64
	returns  int
}

func (l *readLog) ReadAt(p []byte, off int64) (int, error) {
	n, err := l.r.ReadAt(p, off)
	l.mu.Lock()
	l.reads = append(l.reads, loggedRead{from: off, to: off + int64(n), returns: l.returns})
	l.mu.Unlock()
	return n, err
}

func (l *readLog) callbackReturned() {
	l.mu.Lock()
	l.returns++
	l.mu.Unlock()
}

func TestReadCompanyFactsArchiveReadsEachCompanyAfterTheCallbackBefore(t *testing.T) {
	// The fixture entries, then 70 KiB of padding in a file that the reader
	// skips. The search for the end of the archive reads up to its last
	// 65 KiB, so it then reads no data of a company.
	fixture := readFixture(t, companyFactsZip)
	src, err := zip.NewReader(bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range src.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	pad, err := zw.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pad.Write(make([]byte, 70*1024)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	log := &readLog{r: bytes.NewReader(body)}
	err = sec.ReadCompanyFactsArchive(log, int64(len(body)), func(domain.CIK, []domain.Fact) error {
		log.callbackReturned()
		return nil
	})
	if err != nil {
		t.Fatalf("ReadCompanyFactsArchive() error = %v", err)
	}
	// The data of company k is read after k callbacks returned and before
	// the next one returns.
	for k, f := range zr.File[:len(zr.File)-1] {
		from, err := f.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		to := from + int64(f.CompressedSize64)
		found := false
		for _, r := range log.reads {
			if r.from >= to || r.to <= from {
				continue
			}
			found = true
			if r.returns != k {
				t.Errorf("data of entry %d (%s) read after %d callbacks returned, want %d", k, f.Name, r.returns, k)
			}
		}
		if !found {
			t.Errorf("data of entry %d (%s) was never read", k, f.Name)
		}
	}
}

func TestReadCompanyFactsArchiveStopsAtTheCallbackError(t *testing.T) {
	body := readFixture(t, companyFactsZip)
	stop := errors.New("stop")
	calls := 0
	err := sec.ReadCompanyFactsArchive(bytes.NewReader(body), int64(len(body)), func(domain.CIK, []domain.Fact) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Errorf("error = %v, want the callback error", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestReadCompanyFactsArchiveSkipsOtherFiles(t *testing.T) {
	r := buildZip(t, map[string]string{"readme.txt": "x", "CIK0000000001.json": `{"cik":1,"facts":{}}`})
	var ciks []domain.CIK
	err := sec.ReadCompanyFactsArchive(r, r.Size(), func(cik domain.CIK, _ []domain.Fact) error {
		ciks = append(ciks, cik)
		return nil
	})
	if err != nil || len(ciks) != 1 || ciks[0] != 1 {
		t.Errorf("ciks = %v, error = %v, want [1] and no error", ciks, err)
	}
}

func TestReadCompanyFactsArchiveRejectsBadEntries(t *testing.T) {
	tests := map[string]map[string]string{
		"invalid json":       {"CIK0000000001.json": `{"cik":`},
		"cik differs":        {"CIK0000000001.json": `{"cik":2,"facts":{}}`},
		"invalid fact value": {"CIK0000000001.json": `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"x","val":1,"accn":"a","form":"10-K","filed":"2025-01-01"}]}}}}}`},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			r := buildZip(t, files)
			err := sec.ReadCompanyFactsArchive(r, r.Size(), func(domain.CIK, []domain.Fact) error { return nil })
			if err == nil {
				t.Error("error = nil, want an error")
			}
		})
	}
}

func TestReadCompanyFactsArchiveRejectsAFileThatIsNotAZip(t *testing.T) {
	r := bytes.NewReader([]byte("not a zip"))
	if err := sec.ReadCompanyFactsArchive(r, r.Size(), func(domain.CIK, []domain.Fact) error { return nil }); err == nil {
		t.Error("error = nil, want an error")
	}
}

func TestReadSubmissionsArchiveJoinsTheExtraFiles(t *testing.T) {
	body := readFixture(t, submissionsZip)
	got := map[domain.CIK]sec.Submissions{}
	err := sec.ReadSubmissionsArchive(bytes.NewReader(body), int64(len(body)), func(sub sec.Submissions) error {
		got[sub.Company.CIK] = sub
		return nil
	})
	if err != nil {
		t.Fatalf("ReadSubmissionsArchive() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("calls = %d, want 2 (KO and NVDA)", len(got))
	}
	// KO: 25 recent filings and 58 + 41 in its two extra files. NVDA: 24
	// recent and 88 in its extra file.
	if n := len(got[cikKO].Filings); n != 25+58+41 {
		t.Errorf("KO filings = %d, want 124", n)
	}
	if n := len(got[cikNVDA].Filings); n != 24+88 {
		t.Errorf("NVDA filings = %d, want 112", n)
	}
	if _, ok := findFiling(got[cikNVDA].Filings, "0001012870-99-001954"); !ok {
		t.Error("NVDA filing of 1999 from the extra file is missing")
	}
	if got[cikKO].Company.Name != "COCA COLA CO" {
		t.Errorf("KO name = %q", got[cikKO].Company.Name)
	}
}

func TestReadSubmissionsArchiveRejectsAMissingExtraFile(t *testing.T) {
	r := buildZip(t, map[string]string{
		"CIK0000000001.json": `{"cik":"0000000001","filings":{"recent":{},"files":[{"name":"CIK0000000001-submissions-001.json"}]}}`,
	})
	err := sec.ReadSubmissionsArchive(r, r.Size(), func(sec.Submissions) error { return nil })
	if err == nil {
		t.Error("error = nil, want an error for the missing extra file")
	}
}

func TestReadSubmissionsArchiveStopsAtTheCallbackError(t *testing.T) {
	body := readFixture(t, submissionsZip)
	stop := errors.New("stop")
	calls := 0
	err := sec.ReadSubmissionsArchive(bytes.NewReader(body), int64(len(body)), func(sec.Submissions) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("error = %v, calls = %d, want the callback error after 1 call", err, calls)
	}
}

func TestReadSubmissionsArchiveRejectsAMainFileOfAnotherCompany(t *testing.T) {
	r := buildZip(t, map[string]string{"CIK0000000001.json": `{"cik":"0000000002","filings":{"recent":{}}}`})
	if err := sec.ReadSubmissionsArchive(r, r.Size(), func(sec.Submissions) error { return nil }); err == nil {
		t.Error("error = nil, want an error")
	}
}
