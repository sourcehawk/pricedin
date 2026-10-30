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
// Invalid levers or history give an error that wraps [ErrInvalidInput];
// [Levers.Validate] and [History.Validate] list the bounds. Zero or
// negative EPS is valid: it gives values with reason needs_positive_eps.
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
// # What the UI does with the output
//
//   - Default cases: when levers.scenarios is empty, the bear, base and bull
//     cases are the defaults around the weighted trend and the exit P/E in
//     use, so they change when the exit P/E changes. The UI pins them: after
//     a stock pick or Reset, it copies the defaults of the first evaluation
//     into levers.scenarios and keeps them until the next pick or Reset.
//   - P/E band lines: the panel draws each band line with bandPrice of the
//     TypeScript engine ([BandPrice] here), one point per day.
//   - Compare chart: its axis depends on the metrics the user selects, so
//     the panel calls compareScale of the TypeScript engine ([CompareScale]
//     here) with the selected series.
//
// # Shared cases
//
// testdata/engine/cases.json holds the expected outputs of Evaluate and of
// each exported calculation function for fixed inputs. make
// generate-engine-cases writes the file with tools/engine-oracle, which
// computes the expected outputs from the prototype's own code
// (prototype/pricedin.html) with the v1 corrections, except a few
// hand-written reference values; both engines must reproduce them. Its
// format:
//
//	{"version": 1, "cases": [{"name", "fn", "input", "expected", "tolerance", "note"?, "select"?}]}
//
// The fields:
//
//   - fn is the TypeScript function name; input is an object of its
//     arguments, with keys that match the JSON field names exactly (a
//     runner fails on any other key).
//   - The output, as JSON, must equal expected: numbers within the
//     absolute tolerance, null only with null, objects with the same keys,
//     arrays of the same length.
//   - select, when present, names one field of the output object; only
//     that field is compared with expected.
//   - note says where v1 corrects the prototype for this case, or where a
//     hand-written expected value comes from.
package engine
