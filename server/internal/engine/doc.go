// Package engine is PricedIn's calculation engine: pure functions with no
// I/O that turn a stock's history and the user's levers into every number
// the page shows. The API, and later the screener and the MCP server, use
// it; the web app runs a TypeScript port with the same names in camelCase
// and the same JSON shapes.
//
// # Units
//
// Rates and returns are fractions (0.15 is 15%) in Go and in JSON. Money is
// US dollars. Dates are domain.Date, "YYYY-MM-DD" in JSON. JSON field names
// are camelCase.
//
// # Missing values
//
// A value that cannot be computed is a [Maybe]: {"value": 0.24} or
// {"value": null, "reason": "needs_positive_eps"}. The reasons are the
// Reason constants. A whole section that cannot be computed, such as the
// return grid without positive EPS, is a Maybe too. Output never holds NaN
// or Inf. Input echoes that can be null (a quarter's EPS, a day's TTM EPS)
// stay plain nullable numbers.
//
// # Input
//
// [Evaluate] takes a [History] and [Levers]:
//
//	history: {quarters: [Quarter], daily: [PricePoint]}   oldest first
//	  Quarter: {label, periodEnd, eps, close, dilutedShares, revenue,
//	            operatingIncome, operatingCosts, freeCashFlow}   all nullable but label and periodEnd
//	  PricePoint: {date, close, ttmEps}   ttmEps is the TTM EPS known on that day
//	levers: {price, ttmEps, dividend, targetReturn, holdingYears,
//	         exitPe: {mode: estimated|today|trailing|custom, custom, trailingMonths},
//	         baseline: {mode: weighted|today|trailing|custom, trailingMonths,
//	                    custom: {yearly, rate, rates}},
//	         scenarios: [{name, growth, exitPe}],   empty means the defaults
//	         chartRange: 6M|1Y|3Y}
//
// Invalid levers or history give an error that wraps [ErrInvalidInput].
// Zero or negative EPS is valid: it gives values with reason
// needs_positive_eps.
//
// # Output
//
// [Evaluation] holds, by panel:
//
//   - Levers and top bar: peNow, exitPe (the exit P/E in use with its rule,
//     the estimate, g0, gExit, the typical P/E, the slider maximum),
//     baselines (weighted, today, trailing with its months, custom, the
//     active one), yearByYear and trajectory.
//   - Priced in: required (growth, exit price and EPS needed), gap,
//     verdict, atBaseline (implied price, yearly and total return, exit P/E
//     needed), sensitivity (the lever experiments of the walkthroughs),
//     quarters (TTM EPS, YoY, TTM YoY and weighted trend per quarter) and
//     growthScale.
//   - Return grid: returnGrid (rows, columns, cells with tones, row tags,
//     the YOU cell).
//   - Earnings trajectory: earningsPaths (history, baseline and required
//     paths, the gap and its tone, the scale).
//   - How demanding over time: howDemanding.
//   - Bear, base and bull: scenarios.
//   - Price chart: priceChart (points with P/E and 12-month changes, the
//     scales, the P/E bands, the price-earnings link) and shareCountEvents.
//   - Financials: financials (the last eight quarters, tiles, the compare
//     view and its correlation table).
//
// testdata/engine/evaluation-example.json holds a full input and output.
//
// # Shared cases
//
// testdata/engine/cases.json holds the expected outputs of every exported
// function for fixed inputs. tools/engine-oracle computes them from the
// prototype's own code (prototype/pricedin.html), and both engines must
// reproduce them. Where v1 corrects the prototype, the case says so in its
// note.
package engine
