import { niceStep } from "./core";
import type { Quarter } from "./inputs";
import { constant } from "./pricechart";
import {
  at,
  firstReason,
  newAxis,
  newRange,
  none,
  some,
  type Axis,
  type Maybe,
  type MaybeNumber,
  type Range,
  type Tone,
} from "./value";

/** Metric names a financial metric of the financials panel. */
export type Metric =
  "revenue" | "operatingCosts" | "operatingIncome" | "eps" | "freeCashFlow" | "dilutedShares";

/** allMetrics returns every financial metric in panel order. */
export function allMetrics(): Metric[] {
  return ["revenue", "operatingCosts", "operatingIncome", "eps", "freeCashFlow", "dilutedShares"];
}

/** Rising costs and rising share counts are never good. */
function riseIsBad(m: Metric): boolean {
  return m === "operatingCosts" || m === "dilutedShares";
}

const financialQuarters = 8;

/** FinancialsView is the financials panel over the last eight quarters. */
export interface FinancialsView {
  quarters: QuarterRef[];
  metrics: Record<Metric, MetricSeries>;
  /** Operating income over revenue. */
  operatingMargin: MaybeNumber[];
  /** Operating costs over revenue. */
  costShareOfRevenue: MaybeNumber[];
  tiles: Maybe<Tiles>;
  compare: Compare;
}

/** QuarterRef names a quarter. */
export interface QuarterRef {
  label: string;
  periodEnd: string;
}

/**
 * MetricSeries is one metric per quarter with its changes and their tones.
 * For operating costs, a YoY change is warn when it outgrows revenue by
 * more than 0.5 pp and neutral otherwise.
 */
export interface MetricSeries {
  values: (number | null)[];
  yoy: MaybeNumber[];
  yoyTone: Maybe<Tone>[];
  qoq: MaybeNumber[];
  /** Neutral for a change below 0.0001%. */
  qoqTone: Maybe<Tone>[];
  /** The bar scale from zero; for diluted shares it zooms in on the values instead. */
  scale: Maybe<Range>;
}

/** Tiles compare the latest quarter with the same quarter a year earlier. */
export interface Tiles {
  label: string;
  compareLabel: string;
  revenue: Tile;
  operatingCosts: Tile;
  operatingIncome: Tile;
  operatingMargin: MarginTile;
  eps: Tile;
  freeCashFlow: Tile;
  dilutedShares: Tile;
}

/** Tile is one metric of the tiles. */
export interface Tile {
  latest: MaybeNumber;
  change: MaybeNumber;
  tone: Maybe<Tone>;
}

/** MarginTile is the operating margin tile; change is in fractions (0.01 is one pp). */
export interface MarginTile {
  latest: MaybeNumber;
  previous: MaybeNumber;
  change: MaybeNumber;
  tone: Maybe<Tone>;
}

/** Compare is the compare view: each metric's change since the first quarter and from quarter to quarter, and how those changes correlate. */
export interface Compare {
  sinceStart: Record<Metric, MaybeNumber[]>;
  quarterOverQuarter: Record<Metric, MaybeNumber[]>;
  /** Every pair of metrics, in allMetrics order, a before b. */
  correlations: PairCorrelation[];
  /** Quarter-to-quarter changes per metric. */
  changes: number;
}

/** PairCorrelation is the correlation of two metrics' quarter-to-quarter changes, over the quarters where both changes exist. */
export interface PairCorrelation {
  a: Metric;
  b: Metric;
  result: Maybe<PairCorrelationV>;
}

/** PairCorrelationV is a correlation and its class. */
export interface PairCorrelationV {
  correlation: number;
  /** strongSame (0.6 or more), moderateSame (0.3 or more), weak, moderateOpposite (above -0.6) or strongOpposite. */
  class: "strongSame" | "moderateSame" | "weak" | "moderateOpposite" | "strongOpposite";
}

function valueOf(m: Metric, q: Quarter): number | null {
  return q[m];
}

function change(a: number | null, b: number | null): MaybeNumber {
  if (a === null || b === null) {
    return none("missing_data");
  }
  if (!(b > 0)) {
    return none("non_positive_base");
  }
  return some(a / b - 1);
}

function ratio(a: number | null, b: number | null): MaybeNumber {
  if (a === null || b === null) {
    return none("missing_data");
  }
  if (!(b > 0)) {
    return none("non_positive_base");
  }
  return some(a / b);
}

function riseTone(m: Metric, y: number): Tone {
  return y >= 0 !== riseIsBad(m) ? "good" : "bad";
}

function perMetric<T>(f: (m: Metric) => T): Record<Metric, T> {
  return {
    revenue: f("revenue"),
    operatingCosts: f("operatingCosts"),
    operatingIncome: f("operatingIncome"),
    eps: f("eps"),
    freeCashFlow: f("freeCashFlow"),
    dilutedShares: f("dilutedShares"),
  };
}

/** financials returns the financials panel for the last eight quarters. */
export function financials(quarters: readonly Quarter[]): FinancialsView {
  const w = quarters.slice(Math.max(0, quarters.length - financialQuarters));
  const values = (m: Metric): (number | null)[] => w.map((q) => valueOf(m, q));
  const yoyOf = (d: readonly (number | null)[]): MaybeNumber[] =>
    d.map((v, i) => (i >= 4 ? change(v, at(d, i - 4)) : none("insufficient_history")));
  const rev = values("revenue");
  const revYoy = yoyOf(rev);
  const metrics = perMetric((m): MetricSeries => {
    const d = values(m);
    const yoy = yoyOf(d);
    const yoyTone = yoy.map((y, i): Maybe<Tone> => {
      if (y.value === null) {
        return y;
      }
      if (m === "operatingCosts") {
        const r = at(revYoy, i).value;
        return some(r !== null && y.value > r + 0.005 ? "warn" : "neutral");
      }
      return some(riseTone(m, y.value));
    });
    const qoq = d.map((v, i) => (i >= 1 ? change(v, at(d, i - 1)) : none("insufficient_history")));
    const qoqTone = qoq.map((q): Maybe<Tone> => {
      if (q.value === null) {
        return q;
      }
      return some(Math.abs(q.value) < 1e-6 ? "neutral" : riseTone(m, q.value));
    });
    return { values: d, yoy, yoyTone, qoq, qoqTone, scale: metricScale(m, d) };
  });
  const sinceStart = perMetric((m): MaybeNumber[] => {
    const d = values(m);
    return d.map((v) => {
      const base = d[0] ?? null;
      if (v === null || base === null) {
        return none("missing_data");
      }
      if (!(base > 0)) {
        return none("non_positive_base");
      }
      return some(v / base - 1);
    });
  });
  const order = allMetrics();
  const correlations: PairCorrelation[] = [];
  for (let a = 0; a < order.length; a++) {
    for (let b = a + 1; b < order.length; b++) {
      const ma = at(order, a);
      const mb = at(order, b);
      correlations.push(pairCorrelation(ma, mb, metrics[ma].qoq, metrics[mb].qoq));
    }
  }
  const operatingMargin = w.map((q) => ratio(q.operatingIncome, q.revenue));
  return {
    quarters: w.map((q) => ({ label: q.label, periodEnd: q.periodEnd })),
    metrics,
    operatingMargin,
    costShareOfRevenue: w.map((q) => ratio(q.operatingCosts, q.revenue)),
    tiles: tiles(w, rev, values, operatingMargin),
    compare: {
      sinceStart,
      quarterOverQuarter: perMetric((m) => metrics[m].qoq),
      correlations,
      changes: Math.max(0, w.length - 1),
    },
  };
}

function metricScale(m: Metric, d: readonly (number | null)[]): Maybe<Range> {
  let lo = Infinity;
  let hi = -Infinity;
  for (const v of d) {
    if (v !== null) {
      lo = Math.min(lo, v);
      hi = Math.max(hi, v);
    }
  }
  if (lo === Infinity) {
    return none("missing_data");
  }
  if (m === "dilutedShares") {
    const pad = Math.max((hi - lo) * 0.35, hi * 0.004);
    return some(newRange(lo - pad, hi + pad));
  }
  return some(newRange(Math.min(0, lo * 1.12), Math.max(0, hi * 1.12)));
}

function pairCorrelation(
  a: Metric,
  b: Metric,
  qa: readonly MaybeNumber[],
  qb: readonly MaybeNumber[],
): PairCorrelation {
  const xs: number[] = [];
  const ys: number[] = [];
  for (let i = 1; i < qa.length; i++) {
    const x = at(qa, i).value;
    const y = at(qb, i).value;
    if (x !== null && y !== null) {
      xs.push(x);
      ys.push(y);
    }
  }
  if (xs.length === 0) {
    return { a, b, result: none("missing_data") };
  }
  if (constant(xs) || constant(ys)) {
    return { a, b, result: none("zero_variance") };
  }
  const n = xs.length;
  let ma = 0;
  let mb = 0;
  for (const x of xs) {
    ma += x;
  }
  for (const y of ys) {
    mb += y;
  }
  ma /= n;
  mb /= n;
  let sab = 0;
  let saa = 0;
  let sbb = 0;
  for (let i = 0; i < n; i++) {
    const dx = at(xs, i) - ma;
    const dy = at(ys, i) - mb;
    sab += dx * dy;
    saa += dx * dx;
    sbb += dy * dy;
  }
  if (saa === 0 || sbb === 0) {
    return { a, b, result: none("zero_variance") };
  }
  const r = sab / Math.sqrt(saa * sbb);
  let cls: PairCorrelationV["class"] = "strongOpposite";
  if (r >= 0.6) {
    cls = "strongSame";
  } else if (r >= 0.3) {
    cls = "moderateSame";
  } else if (r > -0.3) {
    cls = "weak";
  } else if (r > -0.6) {
    cls = "moderateOpposite";
  }
  return { a, b, result: some({ correlation: r, class: cls }) };
}

function tiles(
  w: readonly Quarter[],
  rev: readonly (number | null)[],
  values: (m: Metric) => (number | null)[],
  margin: readonly MaybeNumber[],
): Maybe<Tiles> {
  if (w.length < 5) {
    return none("insufficient_history");
  }
  const i = w.length - 1;
  const j = w.length - 5;
  const revChange = change(at(rev, i), at(rev, j));
  const tile = (m: Metric): Tile => {
    const d = values(m);
    const latest = at(d, i);
    const c = change(latest, at(d, j));
    let t: Maybe<Tone>;
    if (c.value === null) {
      t = c;
    } else if (m === "operatingCosts") {
      t = some(revChange.value !== null && c.value > revChange.value + 0.005 ? "warn" : "neutral");
    } else if (Math.abs(c.value) < 1e-9) {
      t = some("neutral");
    } else {
      t = some(riseTone(m, c.value));
    }
    return { latest: latest === null ? none("missing_data") : some(latest), change: c, tone: t };
  };
  const latest = at(margin, i);
  const previous = at(margin, j);
  let marginChange: MaybeNumber;
  let marginTone: Maybe<Tone>;
  if (latest.value !== null && previous.value !== null) {
    const c = latest.value - previous.value;
    marginChange = some(c);
    marginTone = some(Math.abs(c) >= 1e-9 ? riseTone("revenue", c) : "neutral");
  } else {
    marginChange = none(firstReason(latest, previous));
    marginTone = marginChange;
  }
  return some({
    label: at(w, i).label,
    compareLabel: at(w, j).label,
    revenue: tile("revenue"),
    operatingCosts: tile("operatingCosts"),
    operatingIncome: tile("operatingIncome"),
    operatingMargin: { latest, previous, change: marginChange, tone: marginTone },
    eps: tile("eps"),
    freeCashFlow: tile("freeCashFlow"),
    dilutedShares: tile("dilutedShares"),
  });
}

/** compareScale returns the axis of the compare chart for the values of the selected metrics. It always includes 0; null and non-finite values are ignored. */
export function compareScale(values: readonly (number | null)[]): Axis {
  let mn = 0;
  let mx = 0;
  for (const v of values) {
    if (v !== null && Number.isFinite(v)) {
      mn = Math.min(mn, v);
      mx = Math.max(mx, v);
    }
  }
  let rng = mx - mn;
  if (rng === 0) {
    rng = 1;
  }
  const st = niceStep(rng, 5);
  return newAxis(Math.floor(mn / st) * st, Math.ceil(mx / st) * st, st);
}
