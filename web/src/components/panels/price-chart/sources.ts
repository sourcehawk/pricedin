import type { PriceFeed, PriceSource, QuarterSources, Source, SourcedNumber } from "@/api/types";
import { isEstimate, newestFiling } from "@/components/Sourced";
import type { ChartPoint } from "@/engine";

/**
 * closeSourced returns the close of date with its source: the profile's
 * latest close on its own day when it is close, and otherwise the feed's
 * close of that day. It is null without a feed.
 */
export function closeSourced(
  date: string,
  close: number,
  feed: PriceFeed | null,
  latest: SourcedNumber | null,
): SourcedNumber | null {
  if (latest !== null && latest.asOf === date && latest.value === close) {
    return latest;
  }
  if (feed === null) {
    return null;
  }
  let splitFactor = 1;
  for (const s of feed.splits) {
    if (date < s.date) {
      splitFactor *= s.ratio;
    }
  }
  const source: PriceSource = {
    kind: "price",
    provider: feed.provider,
    symbol: feed.symbol,
    closeDate: date,
    rawClose: close * splitFactor,
    splitFactor,
  };
  return { value: close, source, asOf: date, asOfLabel: "as of close" };
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
