import { niceStep } from "./core";
import { addDays, addMonths } from "./dates";
import { nearest } from "./grid";
import {
  dayRows,
  quarterRows,
  type ChartRange,
  type DayRow,
  type PricePoint,
  type Quarter,
} from "./inputs";
import {
  at,
  finite,
  firstReason,
  newAxis,
  newRange,
  none,
  some,
  toFixed,
  type Axis,
  type Maybe,
  type MaybeNumber,
  type Range,
  type Reason,
} from "./value";

/** ShareEvent is a quarter in which the diluted share count changed by 2% or more against the quarter before. */
export interface ShareEvent {
  label: string;
  periodEnd: string;
  from: number;
  to: number;
  change: number;
  /** More shares is dilution, fewer is a buyback. */
  kind: "dilution" | "buyback";
}

const shareEventMin = 0.02;

/** shareCountEvents returns the share-count events, oldest first. Quarters without a count on either side are skipped. */
export function shareCountEvents(input: readonly Quarter[]): ShareEvent[] {
  const quarters = quarterRows(input);
  const out: ShareEvent[] = [];
  for (let i = 1; i < quarters.length; i++) {
    const a = at(quarters, i - 1).dilutedShares;
    const q = at(quarters, i);
    const b = q.dilutedShares;
    if (a === null || b === null || !(a > 0)) {
      continue;
    }
    const c = b / a - 1;
    if (Math.abs(c) < shareEventMin) {
      continue;
    }
    out.push({
      label: q.label,
      periodEnd: q.periodEnd,
      from: a,
      to: b,
      change: c,
      kind: c > 0 ? "dilution" : "buyback",
    });
  }
  return out;
}

/** Link is a correlation with its class. */
export interface Link {
  correlation: number;
  /** Strong (0.6 or more), moderate (0.3 to 0.6), weak, or negative (-0.3 or less). */
  class: "strong" | "moderate" | "weak" | "negative";
}

const maxBands = 50;
const minLinkPoints = 20;

/**
 * correlation returns the Pearson correlation of xs and ys (equal length) as
 * a price-earnings link. It is missing with too_few_points below 20 points
 * and with zero_variance when either series is constant.
 */
export function correlation(xs: readonly number[], ys: readonly number[]): Maybe<Link> {
  if (xs.length < minLinkPoints) {
    return none("too_few_points");
  }
  if (constant(xs) || constant(ys)) {
    return none("zero_variance");
  }
  const r = pearson(xs, ys);
  if (!Number.isFinite(r)) {
    return none("zero_variance");
  }
  let cls: Link["class"] = "weak";
  if (r <= -0.3) {
    cls = "negative";
  } else if (r >= 0.6) {
    cls = "strong";
  } else if (r >= 0.3) {
    cls = "moderate";
  }
  return some({ correlation: r, class: cls });
}

/** constant reports whether every value equals the first; rounding can leave a constant series off its mean. */
export function constant(vs: readonly number[]): boolean {
  return vs.every((v) => v === vs[0]);
}

function pearson(xs: readonly number[], ys: readonly number[]): number {
  const n = xs.length;
  let mx = 0;
  let my = 0;
  for (const x of xs) {
    mx += x;
  }
  for (const y of ys) {
    my += y;
  }
  mx /= n;
  my /= n;
  let sxy = 0;
  let sxx = 0;
  let syy = 0;
  for (let i = 0; i < n; i++) {
    const dx = at(xs, i) - mx;
    const dy = at(ys, i) - my;
    sxy += dx * dy;
    sxx += dx * dx;
    syy += dy * dy;
  }
  return sxy / Math.sqrt(sxx * syy);
}

/** Bands are the P/E bands: lines at fixed multiples of TTM EPS that cover the P/E range of the visible period. */
export interface Bands {
  multiples: Band[];
  /** The P/E on the first day. */
  firstPe: MaybeNumber;
  /** The P/E on the last day. */
  currentPe: MaybeNumber;
  /** The band the price sits in today; missing when it sits on a line or outside the bands. */
  current: Maybe<BandPair>;
  /** The multiple nearest to today's P/E. */
  nearest: MaybeNumber;
  /** The multiples that the P/E crossed between the first and the last day, by more than 0.3. */
  crossed: number[];
}

/** Band is one multiple with its price today. */
export interface Band {
  multiple: number;
  /** Today's TTM EPS times the multiple. */
  priceToday: MaybeNumber;
  /** "above", "below" or "at" (within 1%) today's close. */
  vsPrice: Maybe<"above" | "below" | "at">;
}

/** BandPair is the band between two neighboring multiples. */
export interface BandPair {
  lower: number;
  upper: number;
}

/**
 * peBands returns the P/E bands for the visible points, about 5 of them.
 * bandPrice gives the line of a band on each day. The bands are missing
 * when no point has a positive TTM EPS.
 */
export function peBands(input: readonly PricePoint[]): Maybe<Bands> {
  const points = dayRows(input);
  let lo = Infinity;
  let hi = -Infinity;
  for (const d of points) {
    const pe = pointPe(d).value;
    if (pe !== null && pe > 0) {
      lo = Math.min(lo, pe);
      hi = Math.max(hi, pe);
    }
  }
  if (lo === Infinity) {
    return none("needs_positive_eps");
  }
  const pLo = lo * 0.9;
  const pHi = hi * 1.05;
  // The powers of ten widen until the largest step gives at most 5 bands
  // and the smallest at least 3, so a P/E in the billions (a near-zero
  // EPS) still gets a few bands.
  let kLo = -1;
  let kHi = 3;
  while ((pHi - pLo) / (5 * Math.pow(10, kHi)) > 5 && kHi < 400) {
    kHi++;
  }
  while ((pHi - pLo) / Math.pow(10, kLo) < 3 && kLo > -400) {
    kLo--;
  }
  let stp = 1;
  let best = 1e9;
  let digits = 2;
  for (let k = kLo; k <= kHi; k++) {
    for (const f of [1, 2, 2.5, 5]) {
      const c = f * Math.pow(10, k);
      const n = Math.floor(pHi / c) - Math.ceil(pLo / c) + 1;
      if (n < 3) {
        continue;
      }
      const sc = Math.abs(n - 5);
      if (sc < best) {
        best = sc;
        stp = c;
        // Enough decimals for the step: 0.025 needs three.
        digits = f === 2.5 ? Math.max(2, 1 - k) : Math.max(2, -k);
      }
    }
  }
  const ms: number[] = [];
  // The end slack shrinks with the step, so a tiny P/E range does not fill up.
  const slack = Math.min(1e-9, stp * 1e-6);
  // At a huge P/E a step can be too small to change m.
  for (
    let m = Math.ceil(pLo / stp) * stp;
    m <= pHi + slack && ms.length < maxBands && m + stp > m;
    m += stp
  ) {
    if (m > 0) {
      ms.push(toFixed(m, digits));
    }
  }

  const first = at(points, 0);
  const last = at(points, points.length - 1);
  const firstPe = pointPe(first);
  const currentPe = pointPe(last);
  const multiples = ms.map((m): Band => {
    if (currentPe.value === null || last.ttmEps === null) {
      return {
        multiple: m,
        priceToday: currentPe,
        vsPrice: currentPe.value === null ? currentPe : none("missing_data"),
      };
    }
    const v = last.ttmEps * m;
    const vs = Math.abs(v / last.close - 1) < 0.01 ? "at" : v > last.close ? "above" : "below";
    return { multiple: m, priceToday: some(v), vsPrice: some(vs) };
  });
  if (currentPe.value === null) {
    return some({
      multiples,
      firstPe,
      currentPe,
      current: currentPe,
      nearest: currentPe,
      crossed: [],
    });
  }
  const now = currentPe.value;
  const up = ms.findIndex((m) => m >= now);
  let down = -1;
  ms.forEach((m, i) => {
    if (m <= now) {
      down = i;
    }
  });
  const current: Maybe<BandPair> =
    up >= 0 && down >= 0 && at(ms, up) !== at(ms, down)
      ? some({ lower: at(ms, down), upper: at(ms, up) })
      : none("not_applicable");
  const near: MaybeNumber = ms.length > 0 ? some(at(ms, nearest(ms, now))) : none("not_applicable");
  const crossed: number[] = [];
  if (firstPe.value !== null) {
    const a = firstPe.value;
    const cLo = Math.min(a, now);
    const cHi = Math.max(a, now);
    for (const m of ms) {
      if (m > cLo + 0.3 && m < cHi - 0.3) {
        crossed.push(m);
      }
    }
  }
  return some({ multiples, firstPe, currentPe, current, nearest: near, crossed });
}

/**
 * bandPrice returns the price on the line of a P/E band on one day: the
 * day's TTM EPS times the band's multiple. It is missing when the TTM EPS
 * is null or undefined (missing_data) or not positive (needs_positive_eps).
 */
export function bandPrice(ttmEps: number | null | undefined, multiple: number): MaybeNumber {
  if (ttmEps === null || ttmEps === undefined) {
    return none("missing_data");
  }
  if (!(ttmEps > 0)) {
    return none("needs_positive_eps");
  }
  return some(ttmEps * multiple);
}

function pointPe(d: DayRow): MaybeNumber {
  if (d.ttmEps === null) {
    return none("missing_data");
  }
  if (!(d.ttmEps > 0)) {
    return none("needs_positive_eps");
  }
  return finite(d.close / d.ttmEps, "needs_positive_eps");
}

/** Chart is the price chart of the visible range. */
export interface Chart {
  range: ChartRange;
  /** The first visible day. */
  start: string;
  /** The last day. */
  end: string;
  points: ChartPoint[];
  /**
   * The left axis. The overlays have free scales that fill the plot: TTM
   * EPS, P/E, and the two 12-month changes, which share changeRange and
   * always include 0.
   */
  priceAxis: Axis;
  epsRange: Maybe<Range>;
  peRange: Maybe<Range>;
  changeRange: Range;
  /** Over the range. */
  priceChange: number;
  high: number;
  low: number;
  /** TTM EPS over the range. */
  epsChange: MaybeNumber;
  peStart: MaybeNumber;
  peEnd: MaybeNumber;
  /** "expanded" (the P/E rose by more than 5%), "contracted" (fell by more than 5%) or "steady". */
  peTrend: Maybe<"expanded" | "contracted" | "steady">;
  /**
   * The part of the price gain that came from higher TTM EPS, on a log
   * scale; the rest came from a higher P/E. It exists only when both the
   * price and TTM EPS rose.
   */
  earningsShareOfGain: MaybeNumber;
  last12Months: Last12Months;
  /** The price-earnings link over the range. */
  link: Maybe<Link>;
  bands: Maybe<Bands>;
  /** Within the range. */
  shareCountEvents: ShareEvent[];
}

/** ChartPoint is one day of the chart. */
export interface ChartPoint {
  date: string;
  close: number;
  ttmEps: number | null;
  pe: MaybeNumber;
  /** Against the last close at least 365 days earlier. */
  priceChange12m: MaybeNumber;
  /** TTM EPS against the same day. */
  epsGrowth12m: MaybeNumber;
}

/** Last12Months are the 12-month changes on the last day. */
export interface Last12Months {
  priceChange: MaybeNumber;
  epsGrowth: MaybeNumber;
  /** The change in P/E that the two imply. */
  peChange: MaybeNumber;
}

const rangeMonths: Record<ChartRange, number> = { "6M": 6, "1Y": 12, "3Y": 36 };

/**
 * priceChart returns the price chart for the range; an empty range is 3Y.
 * The 12-month changes use the days before the range too. It is missing
 * without daily data.
 */
export function priceChart(
  days: readonly PricePoint[],
  quarters: readonly Quarter[],
  chartRange: ChartRange | "",
): Maybe<Chart> {
  const daily = dayRows(days);
  if (daily.length === 0) {
    return none("insufficient_history");
  }
  const rng: ChartRange = chartRange === "" ? "3Y" : chartRange;
  const all = enrich(daily);
  const end = at(daily, daily.length - 1).date;
  const start = addMonths(end, -rangeMonths[rng]);
  const from = daily.findIndex((d) => d.date >= start);
  const pts = all.slice(from);
  const vis = daily.slice(from);

  let lo = Infinity;
  let hi = -Infinity;
  for (const p of pts) {
    lo = Math.min(lo, p.close);
    hi = Math.max(hi, p.close);
  }
  const mn = lo * 0.9;
  const mx = hi * 1.06;
  const st = niceStep(mx - mn, 5);
  const eps: number[] = [];
  const pes: number[] = [];
  const changes = [0];
  for (const p of pts) {
    if (p.ttmEps !== null) {
      eps.push(p.ttmEps);
    }
    if (p.pe.value !== null) {
      pes.push(p.pe.value);
    }
    if (p.priceChange12m.value !== null) {
      changes.push(p.priceChange12m.value);
    }
    if (p.epsGrowth12m.value !== null) {
      changes.push(p.epsGrowth12m.value);
    }
  }
  const changeRange = fit(changes, "not_applicable");

  const f = at(pts, 0);
  const l = at(pts, pts.length - 1);
  let epsChange: MaybeNumber;
  if (f.ttmEps === null || l.ttmEps === null) {
    epsChange = none("missing_data");
  } else if (f.ttmEps > 0) {
    epsChange = some(l.ttmEps / f.ttmEps - 1);
  } else {
    epsChange = none("needs_positive_eps");
  }
  let peTrend: Chart["peTrend"];
  if (f.pe.value !== null && l.pe.value !== null) {
    const a = f.pe.value;
    const b = l.pe.value;
    peTrend = some(b > a * 1.05 ? "expanded" : b < a * 0.95 ? "contracted" : "steady");
  } else {
    peTrend = none(firstReason(f.pe, l.pe));
  }
  let earningsShareOfGain: MaybeNumber = none("not_applicable");
  if (f.ttmEps === null || l.ttmEps === null) {
    earningsShareOfGain = none("missing_data");
  } else if (f.ttmEps > 0 && l.ttmEps > 0) {
    const lp = Math.log(l.close / f.close);
    const le = Math.log(l.ttmEps / f.ttmEps);
    if (lp > 0 && le > 0) {
      earningsShareOfGain = some(le / lp);
    }
  }
  const r12 = l.priceChange12m;
  const g12 = l.epsGrowth12m;
  let peChange: MaybeNumber;
  if (r12.value === null || g12.value === null) {
    peChange = none(firstReason(r12, g12));
  } else if (l.ttmEps === null || !(l.ttmEps > 0)) {
    peChange = none("needs_positive_eps");
  } else {
    peChange = some((1 + r12.value) / (1 + g12.value) - 1);
  }
  const xs: number[] = [];
  const ys: number[] = [];
  for (const p of pts) {
    if (p.priceChange12m.value !== null && p.epsGrowth12m.value !== null) {
      xs.push(p.priceChange12m.value);
      ys.push(p.epsGrowth12m.value);
    }
  }
  const chartStart = f.date;
  return some({
    range: rng,
    start: chartStart,
    end,
    points: pts,
    priceAxis: newAxis(Math.floor(mn / st) * st, Math.ceil(mx / st) * st, st),
    epsRange: fit(eps, "missing_data"),
    peRange: fit(pes, "needs_positive_eps"),
    changeRange: changeRange.value ?? { min: 0, max: 0 },
    priceChange: l.close / f.close - 1,
    high: hi,
    low: lo,
    epsChange,
    peStart: f.pe,
    peEnd: l.pe,
    peTrend,
    earningsShareOfGain,
    last12Months: { priceChange: r12, epsGrowth: g12, peChange },
    link: correlation(xs, ys),
    bands: peBands(vis),
    shareCountEvents: shareCountEvents(quarters).filter(
      (e) => e.periodEnd >= chartStart && e.periodEnd <= end,
    ),
  });
}

/** enrich adds the P/E and the 12-month changes to every daily point. */
function enrich(daily: readonly DayRow[]): ChartPoint[] {
  const first = at(daily, 0);
  let j = 0;
  return daily.map((d): ChartPoint => {
    const back = addDays(d.date, -365);
    while (j < daily.length - 1 && !(at(daily, j + 1).date > back)) {
      j++;
    }
    const pe = pointPe(d);
    if (first.date > back) {
      const missing = none("insufficient_history");
      return {
        date: d.date,
        close: d.close,
        ttmEps: d.ttmEps,
        pe,
        priceChange12m: missing,
        epsGrowth12m: missing,
      };
    }
    const ref = at(daily, j);
    let epsGrowth12m: MaybeNumber;
    if (d.ttmEps === null || ref.ttmEps === null) {
      epsGrowth12m = none("missing_data");
    } else if (!(ref.ttmEps > 0)) {
      epsGrowth12m = none("needs_positive_eps");
    } else {
      epsGrowth12m = some(d.ttmEps / ref.ttmEps - 1);
    }
    return {
      date: d.date,
      close: d.close,
      ttmEps: d.ttmEps,
      pe,
      priceChange12m: some(d.close / ref.close - 1),
      epsGrowth12m,
    };
  });
}

/** fit returns a free scale that fills the plot with 8% padding; missing with reason when there are no values. */
function fit(vals: readonly number[], reason: Reason): Maybe<Range> {
  if (vals.length === 0) {
    return none(reason);
  }
  let a = Infinity;
  let b = -Infinity;
  for (const v of vals) {
    a = Math.min(a, v);
    b = Math.max(b, v);
  }
  let r = b - a;
  if (r === 0) {
    r = Math.abs(b);
  }
  if (r === 0) {
    r = 1;
  }
  return some(newRange(a - r * 0.08, b + r * 0.08));
}
