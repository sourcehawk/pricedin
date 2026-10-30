// Number formats of the page, the same as the prototype's. Rates and returns
// are fractions; every format shows "n/a" for a missing or infinite value.

type Value = number | null | undefined;

function usable(x: Value): x is number {
  return x !== null && x !== undefined && Number.isFinite(x);
}

/** pct shows a fraction as a percent: 0.148 is "14.8%". */
export function pct(x: Value, decimals = 1): string {
  return usable(x) ? `${(x * 100).toFixed(decimals)}%` : "n/a";
}

/** signedPct is pct with a plus sign before a gain; a value that rounds to zero has no sign. */
export function signedPct(x: Value, decimals = 1): string {
  if (!usable(x)) {
    return "n/a";
  }
  const t = (x * 100).toFixed(decimals);
  if (Number(t) === 0) {
    return `${(0).toFixed(decimals)}%`;
  }
  return `${x > 0 ? "+" : ""}${t}%`;
}

/** pp shows a difference of two fractions in signed percentage points: "-9.1 pp". */
export function pp(x: Value): string {
  return usable(x) ? `${x >= 0 ? "+" : ""}${(x * 100).toFixed(1)} pp` : "n/a";
}

/** usd shows US dollars: "$1,234.50", "-$3.20". */
export function usd(x: Value, decimals = 2): string {
  if (!usable(x)) {
    return "n/a";
  }
  const abs = Math.abs(x).toLocaleString("en-US", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
  return `${x < 0 ? "-" : ""}$${abs}`;
}

/** times shows a multiple such as a P/E: "25.0×". */
export function times(x: Value, decimals = 1): string {
  return usable(x) ? `${x.toFixed(decimals)}×` : "n/a";
}

/** years shows a holding period: "1 year", "3 years". */
export function years(n: number): string {
  return `${String(n)} ${n === 1 ? "year" : "years"}`;
}
