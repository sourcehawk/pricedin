package fundamentals

import "github.com/sourcehawk/pricedin/server/internal/domain"

// TTM returns the trailing-twelve-month sum of m over the four quarters
// that end with s.Quarters[at], with MethodTTM and the inputs of all four.
// It reports false when at has fewer than three quarters before it, when a
// quarter lacks m, or for DilutedShares, which does not add up over time.
func TTM(s Series, m Metric, at int) (Value, bool) {
	if m == DilutedShares || at < 3 || at >= len(s.Quarters) {
		return Value{}, false
	}
	out := Value{Method: MethodTTM}
	sum := 0.0
	for _, q := range s.Quarters[at-3 : at+1] {
		v, ok := q.Values[m]
		if !ok {
			return Value{}, false
		}
		sum += v.V
		out.Estimate = out.Estimate || v.Estimate
		out.Inputs = append(out.Inputs, v.Inputs...)
		out.Filed = latestDate(out.Filed, v.Filed)
		out.FirstFiled = latestDate(out.FirstFiled, v.FirstFiled)
	}
	out.V = tidy(sum)
	return out, true
}

// TTMKnownOn returns the TTM of m at the latest quarter whose TTM was known
// on the date on: all four quarters had a FirstFiled on or before it. It
// also returns that quarter's index in s.Quarters, and false when no TTM
// was known yet. The values are the latest filed ones, restated and split
// adjusted, so a history of prices adjusted for splits can be divided by
// them. For values exactly as they were filed by a date, Derive with
// Options.AsOf instead.
func TTMKnownOn(s Series, m Metric, on domain.Date) (int, Value, bool) {
	for at := len(s.Quarters) - 1; at >= 3; at-- {
		if v, ok := TTM(s, m, at); ok && !v.FirstFiled.After(on) {
			return at, v, true
		}
	}
	return 0, Value{}, false
}
