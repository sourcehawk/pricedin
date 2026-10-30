package sec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// ParseDailyIndex decodes a daily master index of EDGAR
// (/Archives/edgar/daily-index/<year>/QTR<n>/master.<yyyymmdd>.idx): every
// filing that SEC disseminated that day, of every form type. The index has no
// report date and no primary document, so those fields stay empty.
func ParseDailyIndex(body []byte) ([]domain.Filing, error) {
	sc := bufio.NewScanner(bytes.NewReader(body))
	inRows := false
	var filings []domain.Filing
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")
		if !inRows {
			// A line of dashes ends the header.
			inRows = strings.HasPrefix(text, "-----")
			continue
		}
		if text == "" {
			continue
		}
		f, err := indexRow(text)
		if err != nil {
			return nil, fmt.Errorf("parse daily index: line %d: %w", line, err)
		}
		filings = append(filings, f)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse daily index: %w", err)
	}
	if !inRows {
		return nil, errors.New("parse daily index: no header")
	}
	return filings, nil
}

// indexRow reads "CIK|Company Name|Form Type|Date Filed|File Name". It takes
// the last three fields from the end, so a "|" in a company name does no harm.
func indexRow(text string) (domain.Filing, error) {
	fields := strings.Split(text, "|")
	if len(fields) < 5 {
		return domain.Filing{}, fmt.Errorf("%d fields, want 5", len(fields))
	}
	n := len(fields)
	cik, err := domain.ParseCIK(fields[0])
	if err != nil {
		return domain.Filing{}, err
	}
	filed, err := time.Parse("20060102", fields[n-2])
	if err != nil {
		return domain.Filing{}, fmt.Errorf("date filed: %w", err)
	}
	// The file name is edgar/data/<cik>/<accession>.txt.
	accession, ok := strings.CutSuffix(path.Base(fields[n-1]), ".txt")
	if !ok || accession == "" {
		return domain.Filing{}, fmt.Errorf("file name %q: want edgar/data/<cik>/<accession>.txt", fields[n-1])
	}
	return domain.Filing{
		Accession: accession,
		CIK:       cik,
		Form:      fields[n-3],
		Filed:     domain.DateOf(filed),
	}, nil
}
