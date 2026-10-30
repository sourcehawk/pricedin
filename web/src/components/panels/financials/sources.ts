import type { QuarterSources, Source, SourcedNumber } from "@/api/types";
import { newestFiling } from "@/components/Sourced";
import { allMetrics, type FinancialsView, type Metric } from "@/engine";

/** MetricSources holds, per metric, one entry per quarter of the view: the value with its source, or null without a value or a source. */
export type MetricSources = Record<Metric, (SourcedNumber | null)[]>;

function byPeriodEnd(quarters: readonly QuarterSources[]): Map<string, QuarterSources> {
  return new Map(quarters.map((q) => [q.periodEnd, q]));
}

/** asOf is the newest filing date among a source's inputs, or fallback when no filing is among them. */
function sourced(value: number, source: Source, fallback: string): SourcedNumber {
  return { value, source, asOf: newestFiling(source)?.filed ?? fallback };
}

/** metricSources gives each value of the view the source that the history reports for its quarter. */
export function metricSources(
  view: FinancialsView,
  quarters: readonly QuarterSources[],
): MetricSources {
  const at = byPeriodEnd(quarters);
  const out = {} as MetricSources;
  for (const m of allMetrics()) {
    out[m] = view.quarters.map((q, i) => {
      const v = view.metrics[m].values[i] ?? null;
      const s = at.get(q.periodEnd)?.[m] ?? null;
      return v === null || s === null ? null : sourced(v, s, q.periodEnd);
    });
  }
  return out;
}

/** marginSources gives each operating margin of the view its inputs: the quarter's operating income and revenue. */
export function marginSources(
  view: FinancialsView,
  quarters: readonly QuarterSources[],
): (SourcedNumber | null)[] {
  const at = byPeriodEnd(quarters);
  return view.quarters.map((q, i) => {
    const v = view.operatingMargin[i]?.value ?? null;
    const s = at.get(q.periodEnd);
    if (v === null || !s?.operatingIncome || !s.revenue) {
      return null;
    }
    const source: Source = {
      kind: "derived",
      method: "derived",
      estimate: false,
      inputs: [s.operatingIncome, s.revenue],
    };
    return sourced(v, source, q.periodEnd);
  });
}

/** LatestFiling is the newest filing behind the figures of the panel. */
export interface LatestFiling {
  /** The quarter that the filing reports. */
  label: string;
  form: string;
  filed: string;
  url: string;
}

/**
 * latestFiling returns the newest filing among the sources of the view's
 * quarters, labeled with the newest quarter that it reports; null without one.
 */
export function latestFiling(
  view: FinancialsView,
  quarters: readonly QuarterSources[],
): LatestFiling | null {
  const at = byPeriodEnd(quarters);
  let best: LatestFiling | null = null;
  for (const q of view.quarters) {
    const s = at.get(q.periodEnd);
    if (s === undefined) {
      continue;
    }
    for (const m of allMetrics()) {
      const src = s[m];
      const f = src === null ? null : newestFiling(src);
      if (f !== null && (best === null || f.filed >= best.filed)) {
        best = { label: q.label, form: f.form, filed: f.filed, url: f.url };
      }
    }
  }
  return best;
}
