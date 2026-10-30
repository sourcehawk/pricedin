import { usd } from "@/components/format";
import type { Metric } from "@/engine";

// Number formats of the financials panel, the same as the prototype's.

type Value = number | null | undefined;

function usable(x: Value): x is number {
  return x !== null && x !== undefined && Number.isFinite(x);
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

/** shareCount shows a share count in billions ("2.15B"), millions ("192.0M") or whole shares. */
export function shareCount(x: Value): string {
  if (!usable(x)) {
    return "n/a";
  }
  if (Math.abs(x) >= 1e9) {
    return `${(x / 1e9).toFixed(2)}B`;
  }
  if (Math.abs(x) >= 1e6) {
    return `${(x / 1e6).toFixed(1)}M`;
  }
  return Math.round(x).toLocaleString("en-US");
}

/** metricValue shows a value of metric m; decimals applies to amounts in billions. */
export function metricValue(m: Metric, x: Value, decimals = 1): string {
  switch (m) {
    case "eps":
      return usd(x, 2);
    case "dilutedShares":
      return shareCount(x);
    default:
      return money(x, decimals);
  }
}
