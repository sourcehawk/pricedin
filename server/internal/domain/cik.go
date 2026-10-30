package domain

import (
	"errors"
	"fmt"
	"strconv"
)

// CIK is the SEC Central Index Key of a filer. Valid keys are positive and
// have at most 10 digits.
type CIK int64

// String returns the 10-digit, zero-padded form that SEC file names use,
// such as "0000320193".
func (c CIK) String() string {
	return fmt.Sprintf("%010d", int64(c))
}

// ParseCIK accepts a CIK of 1 to 10 decimal digits, with or without leading
// zeros. It rejects signs, spaces, prefixes and zero.
func ParseCIK(s string) (CIK, error) {
	if s == "" || len(s) > 10 {
		return 0, fmt.Errorf("parse cik %q: want 1 to 10 digits", s)
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("parse cik %q: want digits only", s)
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cik %q: %w", s, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("parse cik %q: %w", s, errZeroCIK)
	}
	return CIK(n), nil
}

var errZeroCIK = errors.New("cik must be positive")
