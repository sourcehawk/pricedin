import { baselinePath } from "./baselines";
import { growthInRange, niceStep, requiredGrowth } from "./core";
import { quarterRows, type Investment, type Quarter } from "./inputs";
import { Series } from "./series";
import {
  at,
  firstReason,
  median,
  newAxis,
  none,
  some,
  type Axis,
  type Maybe,
  type MaybeNumber,
  type Tone,
} from "./value";

/** historyQuarters is how many quarters before the latest one the history panels show: three years. */
const historyQuarters = 12;

function windowStart(s: Series): number {
  return Math.max(0, s.last - historyQuarters);
}

/** PathsInput is what the earnings trajectory needs. */
export interface PathsInput {
  quarters: readonly Quarter[];
  /** TTM EPS today, where both paths start. */
  eps: number;
  years: number;
  /** The baseline growth. */
  baseline: MaybeNumber;
  /** When not null or absent, the baseline path follows one rate per year (the custom year-by-year baseline). */
  yearlyRates?: readonly number[] | null;
  /** The EPS needed at the exit; the required path runs to it. */
  requiredEps: MaybeNumber;
}

/** Paths is the earnings trajectory: TTM EPS history, then the baseline and the required EPS paths, quarter by quarter. */
export interface Paths {
  history: HistoryPoint[];
  /** Starts today (quartersAhead 0) and ends at the exit. */
  future: FuturePoint[];
  /** Bad when the required path ends above the baseline path, good otherwise. */
  gapTone: Maybe<Tone>;
  /** Covers every plotted point. */
  scale: Axis;
}

/** HistoryPoint is one reported quarter of the trajectory. */
export interface HistoryPoint {
  label: string;
  periodEnd: string;
  ttmEps: MaybeNumber;
  ttmYoy: MaybeNumber;
}

/** FuturePoint is one quarter of the projection. */
export interface FuturePoint {
  quartersAhead: number;
  /** Null when the latest label is not "Q<n> FY<yy>". */
  label: string | null;
  baseline: MaybeNumber;
  required: MaybeNumber;
  /** Baseline minus required. */
  gap: MaybeNumber;
}

/** earningsPaths returns the earnings trajectory. It is missing when EPS is not positive. */
export function earningsPaths(input: PathsInput): Maybe<Paths> {
  if (!(input.eps > 0)) {
    return none("needs_positive_eps");
  }
  const rows = quarterRows(input.quarters);
  const s = new Series(rows);
  const history: HistoryPoint[] = [];
  for (let i = windowStart(s); i <= s.last; i++) {
    const q = at(rows, i);
    history.push({
      label: q.label,
      periodEnd: q.periodEnd,
      ttmEps: at(s.ttm, i),
      ttmYoy: at(s.ttmYoy, i),
    });
  }
  const g = input.baseline;
  const req = input.requiredEps;
  const gv = g.value !== null && growthInRange(g.value) ? g.value : null;
  const gMissing = g.value === null ? g : none("growth_out_of_range");
  const last = s.last >= 0 ? at(rows, s.last).label : "";
  const steps = 4 * input.years;
  const future: FuturePoint[] = [];
  for (let k = 0; k <= steps; k++) {
    const baseline: MaybeNumber =
      gv === null ? gMissing : some(baselinePath(input.eps, gv, k, input.yearlyRates ?? null));
    const required: MaybeNumber =
      req.value === null ? req : some(input.eps * Math.pow(req.value / input.eps, k / steps));
    const gap: MaybeNumber =
      baseline.value !== null && required.value !== null
        ? some(baseline.value - required.value)
        : none(firstReason(baseline, required));
    future.push({
      quartersAhead: k,
      label: s.last >= 0 ? labelAfter(last, k) : null,
      baseline,
      required,
      gap,
    });
  }
  const end = at(future, steps);
  let gapTone: Maybe<Tone>;
  if (end.gap.value === null) {
    gapTone = end.gap;
  } else {
    const endReq = end.required.value ?? 0;
    const endBase = end.baseline.value ?? 0;
    gapTone = some(endReq > endBase ? "bad" : "good");
  }

  let lo = Infinity;
  let hi = -Infinity;
  const add = (v: number | null): void => {
    if (v !== null) {
      lo = Math.min(lo, v);
      hi = Math.max(hi, v);
    }
  };
  for (const h of history) {
    add(h.ttmEps.value);
  }
  for (const f of future) {
    add(f.baseline.value);
  }
  for (const f of future) {
    add(f.required.value);
  }
  add(input.eps);
  let mn = lo - Math.abs(lo) * 0.08;
  const mx = hi + Math.abs(hi) * 0.04;
  const st = niceStep(mx - mn, 5);
  mn = Math.floor(mn / st) * st;
  return some({ history, future, gapTone, scale: newAxis(mn, mx, st) });
}

const quarterLabel = /^Q([1-4]) FY(\d{2})$/;

/** labelAfter returns the label k quarters after a "Q<n> FY<yy>" label, or null for any other label. k = 0 returns the label itself. */
function labelAfter(label: string, k: number): string | null {
  if (k === 0) {
    return label;
  }
  const m = quarterLabel.exec(label);
  if (m === null) {
    return null;
  }
  const idx = Number(m[1]) - 1 + k;
  const year = (Number(m[2]) + Math.floor(idx / 4)) % 100;
  return `Q${String((idx % 4) + 1)} FY${String(year).padStart(2, "0")}`;
}

/**
 * Demanding is "how demanding over time": the growth the price required at
 * each past quarter, with today's levers, against the TTM EPS growth that
 * the company delivered.
 */
export interface Demanding {
  points: DemandingPoint[];
  scale: Axis;
  /** The median required growth over the points. */
  typicalRequired: MaybeNumber;
  /** The most demanding point; the first one wins a tie (values within 1e-9). */
  peak: Maybe<Peak>;
  /** The points, of compared points with both values, where the requirement exceeded the delivered growth. */
  requiredAboveDelivered: number;
  compared: number;
}

/** DemandingPoint is one quarter. The latest one (today) uses the price and TTM EPS levers instead of the quarter-end values. */
export interface DemandingPoint {
  label: string;
  periodEnd: string;
  today: boolean;
  price: MaybeNumber;
  ttmEps: MaybeNumber;
  pe: MaybeNumber;
  required: MaybeNumber;
  /** TTM EPS growth over the year to this quarter. */
  delivered: MaybeNumber;
}

/** Peak is the index and value of the highest required growth. */
export interface Peak {
  index: number;
  growth: number;
}

const peakTie = 1e-9;

/**
 * howDemanding returns the required growth at each quarter of the last three
 * years, had the investment been made then with today's levers.
 */
export function howDemanding(
  quarters: readonly Quarter[],
  inv: Investment,
  exitPe: MaybeNumber,
): Demanding {
  const rows = quarterRows(quarters);
  const s = new Series(rows);
  const points: DemandingPoint[] = [];
  for (let i = windowStart(s); i <= s.last; i++) {
    const q = at(rows, i);
    const today = i === s.last;
    let price: MaybeNumber = some(inv.price);
    let ttmEps: MaybeNumber = some(inv.eps);
    if (!today) {
      price = q.close === null ? none("missing_data") : some(q.close);
      ttmEps = at(s.ttm, i);
    }
    let pe: MaybeNumber;
    let required: MaybeNumber;
    if (price.value === null || ttmEps.value === null) {
      pe = none(firstReason(price, ttmEps));
    } else if (ttmEps.value > 0) {
      pe = some(price.value / ttmEps.value);
    } else {
      pe = none("needs_positive_eps");
    }
    if (exitPe.value === null) {
      required = exitPe;
    } else if (price.value === null || ttmEps.value === null) {
      required = none(firstReason(price, ttmEps));
    } else {
      required = requiredGrowth({ ...inv, price: price.value, eps: ttmEps.value }, exitPe.value);
    }
    points.push({
      label: q.label,
      periodEnd: q.periodEnd,
      today,
      price,
      ttmEps,
      pe,
      required,
      delivered: at(s.ttmYoy, i),
    });
  }

  let mn = 0;
  let mx = 0.05;
  const reqs: number[] = [];
  let compared = 0;
  let requiredAboveDelivered = 0;
  for (const p of points) {
    for (const v of [p.required.value, p.delivered.value]) {
      if (v !== null) {
        mn = Math.min(mn, v);
        mx = Math.max(mx, v);
      }
    }
    reqs.push(p.required.value ?? NaN);
    if (p.required.value !== null && p.delivered.value !== null) {
      compared++;
      if (p.required.value > p.delivered.value) {
        requiredAboveDelivered++;
      }
    }
  }
  const st = niceStep(mx - mn, 5);
  let top = -Infinity;
  for (const v of reqs) {
    if (!Number.isNaN(v)) {
      top = Math.max(top, v);
    }
  }
  // Rounding noise must not move the peak between equal values.
  const peakIndex = reqs.findIndex((v) => !Number.isNaN(v) && v >= top - peakTie);
  return {
    points,
    scale: newAxis(Math.floor(mn / st) * st, Math.ceil(mx / st) * st, st),
    typicalRequired: median(reqs),
    peak:
      peakIndex < 0
        ? none("insufficient_history")
        : some({ index: peakIndex, growth: at(reqs, peakIndex) }),
    requiredAboveDelivered,
    compared,
  };
}
