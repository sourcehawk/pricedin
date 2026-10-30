package sec

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// ReadCompanyFactsArchive reads the bulk company facts archive
// (companyfacts.zip) of the given size from r, one company at a time, and
// calls fn with the kept facts of each company (see ParseCompanyFacts). It
// holds one company's file in memory at a time, so r can be an *os.File of
// the full archive. Some entries hold "{}": fn gets their CIK and no facts.
// Entries with other names than CIK##########.json are skipped. A non-nil
// error from fn stops the read and is returned wrapped.
func ReadCompanyFactsArchive(r io.ReaderAt, size int64, fn func(cik domain.CIK, facts []domain.Fact) error) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("read company facts archive: %w", err)
	}
	for _, f := range zr.File {
		cik, ok := mainFileCIK(f.Name)
		if !ok {
			continue
		}
		body, err := readZipFile(f)
		if err != nil {
			return fmt.Errorf("read company facts archive: %w", err)
		}
		facts, err := parseCompanyFacts(body, cik)
		if err != nil {
			return fmt.Errorf("read company facts archive: %s: %w", f.Name, err)
		}
		if err := fn(cik, facts); err != nil {
			return fmt.Errorf("read company facts archive: %s: %w", f.Name, err)
		}
	}
	return nil
}

// ReadSubmissionsArchive reads the bulk submissions archive (submissions.zip)
// of the given size from r, one company at a time, and calls fn with each
// company's submissions. Filings holds the recent filings and the filings of
// every extra file that the company's file lists. It returns an error when
// such an extra file is not in the archive. A non-nil error from fn stops the
// read and is returned wrapped.
func ReadSubmissionsArchive(r io.ReaderAt, size int64, fn func(Submissions) error) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("read submissions archive: %w", err)
	}
	// The real archive has about a million entries and some thousands of
	// extra files, so only the extra files go into the map.
	extras := map[string]*zip.File{}
	for _, f := range zr.File {
		if isExtraFile(f.Name) {
			extras[f.Name] = f
		}
	}
	for _, f := range zr.File {
		cik, ok := mainFileCIK(f.Name)
		if !ok {
			continue
		}
		sub, err := readArchivedSubmissions(f, cik, extras)
		if err != nil {
			return fmt.Errorf("read submissions archive: %s: %w", f.Name, err)
		}
		if err := fn(sub); err != nil {
			return fmt.Errorf("read submissions archive: %s: %w", f.Name, err)
		}
	}
	return nil
}

func readArchivedSubmissions(f *zip.File, cik domain.CIK, extras map[string]*zip.File) (Submissions, error) {
	body, err := readZipFile(f)
	if err != nil {
		return Submissions{}, err
	}
	sub, err := ParseSubmissions(body)
	if err != nil {
		return Submissions{}, err
	}
	if sub.Company.CIK != cik {
		return Submissions{}, fmt.Errorf("cik %d, want %d", int64(sub.Company.CIK), int64(cik))
	}
	for _, name := range sub.Files {
		extra, ok := extras[name]
		if !ok {
			return Submissions{}, fmt.Errorf("extra file %s not in the archive", name)
		}
		body, err := readZipFile(extra)
		if err != nil {
			return Submissions{}, err
		}
		filings, err := ParseSubmissionsFile(cik, body)
		if err != nil {
			return Submissions{}, fmt.Errorf("%s: %w", name, err)
		}
		sub.Filings = append(sub.Filings, filings...)
	}
	return sub, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	return body, nil
}

// isExtraFile reports whether name has the form
// CIK##########-submissions-###.json.
func isExtraFile(name string) bool {
	rest, ok := strings.CutPrefix(name, "CIK")
	if !ok || len(rest) < 10 {
		return false
	}
	if _, err := domain.ParseCIK(rest[:10]); err != nil {
		return false
	}
	return strings.HasPrefix(rest[10:], "-submissions-") && strings.HasSuffix(rest, ".json")
}

// mainFileCIK returns the CIK of a name of the form CIK##########.json.
func mainFileCIK(name string) (domain.CIK, bool) {
	digits, ok := strings.CutPrefix(name, "CIK")
	if !ok {
		return 0, false
	}
	digits, ok = strings.CutSuffix(digits, ".json")
	if !ok || len(digits) != 10 {
		return 0, false
	}
	cik, err := domain.ParseCIK(digits)
	return cik, err == nil
}
