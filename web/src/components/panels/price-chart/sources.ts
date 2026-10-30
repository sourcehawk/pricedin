import type { QuarterSources, Source, SourcedNumber } from "@/api/types";
import { isEstimate, newestFiling } from "@/components/Sourced";
import type { ChartPoint } from "@/engine";

// The history sends the daily closes split-adjusted, without the provider's
// raw close and split factor of each day, so a close has a source only on a
// day that the profile or the quarters source: the latest close and the
// quarter-end closes. Every other close, such as most highs and lows, shows
// without one.

/**
 * closeSourced returns the close of date with its source: the latest close
 * of the profile, or the close of a quarter that ends on that trading day.
 * It is null on any other day, and when the sourced value is not close.
 */
export function closeSourced(
  date: string,
  close: number,
  quarters: readonly QuarterSources[],
  latest: SourcedNumber | null,
): SourcedNumber | null {
  if (latest !== null && latest.asOf === date && latest.value === close) {
    return latest;
  }
  for (const q of quarters) {
    const s = q.close;
    if (s?.kind === "price" && s.closeDate === date) {
      return { value: close, source: s, asOf: date, asOfLabel: "as of close" };
    }
  }
  return null;
}

/**
 * ttmSourced returns the TTM EPS of a chart day with the sources of the
 * quarters that the engine names for it, as of the newest filing among
 * them when they have one, as the profile dates its TTM EPS. It is an
 * estimate when one of the quarters is. It is null without a TTM EPS, or
 * when a quarter lacks its source.
 */
export function ttmSourced(
  p: ChartPoint,
  quarters: readonly QuarterSources[],
): SourcedNumber | null {
  if (p.ttmEps === null || p.ttmQuarters.length === 0) {
    return null;
  }
  const inputs: Source[] = [];
  let knownOn: string | null = null;
  for (const label of p.ttmQuarters) {
    const q = quarters.find((x) => x.label === label);
    if (q?.eps == null) {
      return null;
    }
    inputs.push(q.eps);
    knownOn = q.ttmEpsKnownOn;
  }
  const source: Source = {
    kind: "derived",
    method: "ttm",
    estimate: inputs.some(isEstimate),
    inputs,
  };
  return { value: p.ttmEps, source, asOf: newestFiling(source)?.filed ?? knownOn ?? p.date };
}
