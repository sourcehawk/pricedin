package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidSymbol means that a string is not a ticker symbol.
var ErrInvalidSymbol = errors.New("domain: invalid ticker symbol")

// SEC symbols reach 7 characters (units such as KCAC-UN) and Massive
// symbols 7 (warrants such as LVWR.WS); 10 leaves room for longer ones.
var symbolPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]{0,9}$`)

// ParseSymbol returns s without surrounding space and in upper case. It
// gives an error that wraps ErrInvalidSymbol unless the result is 1 to 10
// letters, digits, dots or hyphens that start with a letter or digit. It
// accepts both forms of a share class: SEC's BRK-B and the exchanges' BRK.B.
func ParseSymbol(s string) (string, error) {
	symbol := strings.ToUpper(strings.TrimSpace(s))
	if !symbolPattern.MatchString(symbol) {
		return "", fmt.Errorf("parse symbol %q: want 1 to 10 letters, digits, dots or hyphens: %w", s, ErrInvalidSymbol)
	}
	return symbol, nil
}
