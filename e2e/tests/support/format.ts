// The number formats the page shows. The tests format the API's numbers with
// them and compare the text, so a number on the page and in the API agree to
// the shown digits.

type Value = number | null | undefined;

const usable = (x: Value): x is number => typeof x === "number" && Number.isFinite(x);

/** pct shows a fraction as a percent with one decimal: 0.148 is "14.8%". */
export function pct(x: Value): string {
  return usable(x) ? `${(x * 100).toFixed(1)}%` : "n/a";
}

/** pp shows a difference of fractions in signed percentage points: "-9.1 pp". */
export function pp(x: Value): string {
  return usable(x) ? `${x >= 0 ? "+" : ""}${(x * 100).toFixed(1)} pp` : "n/a";
}

/** usd shows US dollars with cents: "$1,234.50"; a nonzero amount that would round to $0.00 shows up to two significant digits: "$0.0042". */
export function usd(x: Value): string {
  if (!usable(x)) {
    return "n/a";
  }
  const a = Math.abs(x);
  const abs =
    a > 0 && a < 0.005
      ? a.toLocaleString("en-US", { maximumSignificantDigits: 2 })
      : a.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return `${x < 0 ? "-" : ""}$${abs}`;
}

/** signedPct shows a fraction as a signed percent: "+96.4%" with 1 decimal, "+96%" with 0. */
export function signedPct(x: number, decimals: number): string {
  const t = (x * 100).toFixed(decimals);
  return Number(t) === 0 ? `${(0).toFixed(decimals)}%` : `${x > 0 ? "+" : ""}${t}%`;
}

/** times shows a multiple such as a P/E with one decimal: "25.0×". */
export function times(x: Value): string {
  return usable(x) ? `${x.toFixed(1)}×` : "n/a";
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** day shows a YYYY-MM-DD date as "30 Sep 2026". */
export function day(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  return `${String(d.getUTCDate())} ${months[d.getUTCMonth()] ?? ""} ${String(d.getUTCFullYear())}`;
}
