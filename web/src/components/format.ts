// Number formats of the page, the same as the prototype's. Rates and returns
// are fractions; every format shows "n/a" for a missing or infinite value.
// The panels format numbers only with these functions.

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

/** points shows the size of a difference of fractions in percentage points, without a sign or a unit: -0.091 is "9.1". */
export function points(x: Value, decimals = 1): string {
  return usable(x) ? Math.abs(x * 100).toFixed(decimals) : "n/a";
}

/**
 * usd shows US dollars: "$1,234.50", "-$3.20". An amount under one cent
 * shows two significant digits, "$0.0042", so that it never reads as $0.00.
 */
export function usd(x: Value, decimals = 2): string {
  if (!usable(x)) {
    return "n/a";
  }
  const a = Math.abs(x);
  const abs =
    a > 0 && a < 0.01
      ? a.toLocaleString("en-US", { maximumSignificantDigits: 2 })
      : a.toLocaleString("en-US", {
          minimumFractionDigits: decimals,
          maximumFractionDigits: decimals,
        });
  return `${x < 0 ? "-" : ""}$${abs}`;
}

/** signedUsd is usd with a plus sign before a gain: "+$1.20", "-$0.40". */
export function signedUsd(x: Value, decimals = 2): string {
  return usable(x) && x >= 0 ? `+${usd(x, decimals)}` : usd(x, decimals);
}

/** money shows a dollar amount in billions ("$1.99B" with 2 decimals), whole millions ("$612M") or whole dollars. */
export function money(x: Value, decimals = 1): string {
  if (!usable(x)) {
    return "n/a";
  }
  const sign = x < 0 ? "-" : "";
  const a = Math.abs(x);
  if (a >= 1e9) {
    return `${sign}$${(a / 1e9).toFixed(decimals)}B`;
  }
  if (a >= 1e6) {
    return `${sign}$${String(Math.round(a / 1e6))}M`;
  }
  return usd(x, 0);
}

/**
 * shareCount shows a share count with four significant digits in millions
 * or billions ("191.7M", "1.030M", "2.150B"), and a count under a million
 * in whole shares. Four digits never hide a change of 2% or more, the size
 * of a share-count event.
 */
export function shareCount(x: Value): string {
  if (!usable(x)) {
    return "n/a";
  }
  const sign = x < 0 ? "-" : "";
  const a = Math.abs(x);
  if (a < 1e6) {
    return `${sign}${Math.round(a).toLocaleString("en-US")}`;
  }
  const m = (a / 1e6).toPrecision(4);
  if (Number(m) < 1000) {
    return `${sign}${m}M`;
  }
  const b = a / 1e9;
  return `${sign}${b < 1000 ? b.toPrecision(4) : Math.round(b).toLocaleString("en-US")}B`;
}

/** times shows a multiple such as a P/E: "25.0×". */
export function times(x: Value, decimals = 1): string {
  return usable(x) ? `${x.toFixed(decimals)}×` : "n/a";
}

/** factor shows a fixed multiple, such as a band or a grid column, as a number without trailing zeros: "20", "22.5", "0.05". */
export function factor(x: Value): string {
  return usable(x) ? String(Number(x.toPrecision(6))) : "n/a";
}

/** multiple is factor with the times sign: "20×", "22.5×". */
export function multiple(x: Value): string {
  return usable(x) ? `${factor(x)}×` : "n/a";
}

/** coefficient shows a correlation coefficient with two decimals: "0.62", "-0.31". */
export function coefficient(x: Value): string {
  if (!usable(x)) {
    return "n/a";
  }
  const t = x.toFixed(2);
  return Number(t) === 0 ? "0.00" : t;
}

/** years shows a holding period: "1 year", "3 years". */
export function years(n: number): string {
  return `${String(n)} ${n === 1 ? "year" : "years"}`;
}
