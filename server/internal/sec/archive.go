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
