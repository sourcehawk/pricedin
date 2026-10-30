// Package fundamentals derives fiscal quarters from SEC facts: quarterly
// values for each metric, split-adjusted per-share values, and TTM sums.
// Every value keeps the filings it came from. The package is pure: it does
// no I/O, so a reprocess can run it on stored facts.
package fundamentals
