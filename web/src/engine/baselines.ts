import {
  maxCustomGrowth,
  minCustomGrowth,
  quarterRows,
  type CustomGrowth,
  type Quarter,
} from "./inputs";
import { niceStep } from "./core";
import { Series } from "./series";
import { at, newAxis, none, some, type Axis, type Maybe, type MaybeNumber } from "./value";

/** Trailing is trailing growth: the compound yearly growth of TTM EPS over months. */
export interface Trailing {
  growth: MaybeNumber;
  /**
   * The look-back in use: the requested months rounded to a multiple of 3,
   * at least 6 and at most maxMonths (when that is 6 or more).
   */
  months: number;
  /** The longest look-back the history allows. */
  maxMonths: number;
  /** TTM EPS at the start of the look-back. */
  fromTtm: MaybeNumber;
  /** TTM EPS today. */
  toTtm: MaybeNumber;
}

const defaultTrailingMonths = 12;

/** trailingGrowth returns trailing growth over the requested months; zero months means 12. */
export function trailingGrowth(quarters: readonly Quarter[], months: number): Trailing {
  return trailing(new Series(quarterRows(quarters)), months);
}

export function trailing(s: Series, requested: number): Trailing {
  const last = s.last;
  const maxMonths = Math.max(0, 3 * (last - 3));
  const months = requested === 0 ? defaultTrailingMonths : requested;
  let m = Math.round(months / 3) * 3;
  m = Math.min(Math.max(6, maxMonths), Math.max(6, m));
  const q = m / 3;
  const fromTtm = last - q >= 0 ? at(s.ttm, last - q) : none("insufficient_history");
  const toTtm = last >= 0 ? at(s.ttm, last) : none("insufficient_history");
  let growth: MaybeNumber;
  if (last - q < 3) {
    growth = none("insufficient_history");
  } else if (fromTtm.value === null) {
    growth = fromTtm;
  } else if (toTtm.value === null) {
    growth = toTtm;
  } else if (!(fromTtm.value > 0) || !(toTtm.value > 0)) {
    growth = none("needs_positive_eps");
  } else {
    growth = some(Math.pow(toTtm.value / fromTtm.value, 12 / m) - 1);
  }
  return { growth, months: m, maxMonths, fromTtm, toTtm };
}

/** Trajectory is the trajectory fill: one growth rate per year that continues last year's change in the weighted trend. */
export interface Trajectory {
  /**
   * Rounded to 0.5 pp and within -50% to 200%, the bounds of a custom rate,
   * so they can fill the custom year-by-year rates. Within those bounds they
   * never exceed highest (rounded down to 0.5 pp).
   */
  rates: number[];
  /** The change of the weighted trend over the last year; 0 without previous. */
  slope: number;
  /** The weighted trend four quarters ago. */
  previous: MaybeNumber;
  /** The highest weighted trend of the last 8 quarters. */
  highest: number;
  /** The trend speeds up and a rate sits at highest. */
  capped: boolean;
}

/** trajectoryFill returns the trajectory over years. It is missing when the history has no weighted trend. */
export function trajectoryFill(quarters: readonly Quarter[], years: number): Maybe<Trajectory> {
  return trajectory(new Series(quarterRows(quarters)), years);
}

export function trajectory(s: Series, years: number): Maybe<Trajectory> {
  const wtd = s.weighted();
  if (wtd.value === null) {
    return wtd;
  }
  let hi = -Infinity;
  for (let k = 0; k < 8; k++) {
    const v = s.weightedAt(s.last - k).value;
    if (v !== null) {
      hi = Math.max(hi, v);
    }
  }
  const previous = s.weightedAt(s.last - 4);
  const slope = previous.value === null ? 0 : wtd.value - previous.value;
  let g = wtd.value;
  let st = slope;
  const rates: number[] = [];
  let capped = false;
  for (let y = 0; y < years; y++) {
    st *= 0.5;
    g = Math.min(hi, Math.max(-0.5, g + st));
    let rate = Math.min(Math.floor(hi * 200) / 2, Math.round(g * 200) / 2);
    rate = Math.max(minCustomGrowth * 100, Math.min(maxCustomGrowth * 100, rate));
    rates.push(rate / 100);
    if (Math.abs(rate / 100 - hi) < 0.006) {
      capped = true;
    }
  }
  return some({ rates, slope, previous, highest: hi, capped: slope > 0 && capped });
}

/**
 * yearRates returns one custom growth rate per year of the holding period.
 * Years without a rate repeat the last rate, or the flat rate when there is
 * none; rates past the holding period are dropped.
 */
export function yearRates(c: CustomGrowth, years: number): number[] {
  const rates = c.rates ?? [];
  const out: number[] = [];
  for (let y = 0; y < years; y++) {
    if (y < rates.length) {
      out.push(at(rates, y));
    } else if (y > 0) {
      out.push(at(out, y - 1));
    } else {
      out.push(c.rate);
    }
  }
  return out;
}

/** yearlyAverage returns the compound average of yearly rates, each above -100%: the flat rate that ends at the same EPS. No rates give 0. */
export function yearlyAverage(rates: readonly number[]): number {
  // Go's math.Pow(1, +Inf) is 1, and JavaScript's is NaN.
  if (rates.length === 0) {
    return 0;
  }
  let prod = 1;
  for (const r of rates) {
    prod *= 1 + r;
  }
  if (!(prod > 0)) {
    return -1;
  }
  return Math.pow(prod, 1 / rates.length) - 1;
}

/**
 * baselinePath returns EPS after k quarters on the baseline path from eps.
 * With yearlyRates (not null, even when empty) the path follows the rate of
 * each year and ignores g; otherwise it compounds g. Both compound within a
 * year.
 */
export function baselinePath(
  eps: number,
  g: number,
  k: number,
  yearlyRates: readonly number[] | null,
): number {
  if (yearlyRates === null) {
    return eps * Math.pow(1 + g, k / 4);
  }
  let e = eps;
  const full = Math.floor(k / 4);
  for (let y = 0; y < full && y < yearlyRates.length; y++) {
    e *= 1 + at(yearlyRates, y);
  }
  const part = k / 4 - full;
  if (part > 0 && full < yearlyRates.length) {
    e *= Math.pow(1 + at(yearlyRates, full), part);
  }
  return e;
}

/**
 * growthScale returns the scale of the growth ruler for the required growth
 * and the baselines. Null and non-finite values are ignored; the scale
 * always spans 0% to 30%, and it has at most about 12 steps.
 */
export function growthScale(values: readonly (number | null)[]): Axis {
  let lo = 0;
  let hi = 0.3;
  for (const v of values) {
    if (v !== null && Number.isFinite(v)) {
      lo = Math.min(lo, v);
      hi = Math.max(hi, v);
    }
  }
  lo = Math.floor((lo - 0.02) * 20) / 20;
  hi = Math.ceil((hi + 0.03) * 20) / 20;
  let step = 0.05;
  if (hi - lo > 0.8) {
    step = 0.2;
  } else if (hi - lo > 0.4) {
    step = 0.1;
  }
  if ((hi - lo) / step > 12) {
    step = niceStep(hi - lo, 6);
  }
  return newAxis(lo, hi, step);
}
