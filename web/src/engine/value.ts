/** Reason says why a value is missing. Every missing value in the output carries one. */
export type Reason =
  /** The value divides by EPS, or by a P/E, that is zero or negative. */
  | "needs_positive_eps"
  /** The dividends alone reach the target return, so no EPS growth is required. */
  | "dividends_reach_target"
  /** The history has too few quarters or days. */
  | "insufficient_history"
  /** An input that the value needs is null. */
  | "missing_data"
  /** A change against a base that is zero or negative. */
  | "non_positive_base"
  /** A growth rate of -100% or lower, or above 1000%. */
  | "growth_out_of_range"
  /** A correlation needs at least 20 points. */
  | "too_few_points"
  /** A correlation of a series that does not vary. */
  | "zero_variance"
  /** The value does not exist for these inputs, such as the custom rate of another baseline. */
  | "not_applicable";

/**
 * Maybe is a value that can be missing: `{value}` or `{value: null, reason}`,
 * the JSON shape of the Go engine's Maybe. A missing value always has a
 * reason; a present one has no reason key.
 */
export type Maybe<T> = { readonly value: T } | Missing;

/** Missing is the missing form of every Maybe. */
export interface Missing {
  readonly value: null;
  readonly reason: Reason;
}

/** MaybeNumber is a number that can be missing (Go's engine.Number). */
export type MaybeNumber = Maybe<number>;

/** some returns a present value. */
export function some<T>(value: T): Maybe<T> {
  return { value };
}

/** none returns a missing value with its reason. */
export function none(reason: Reason): Missing {
  return { value: null, reason };
}

/** isSome reports whether m holds a value. */
export function isSome<T>(m: Maybe<T>): m is { readonly value: T } {
  return m.value !== null;
}

/** firstReason returns the reason of the first missing value among ms; callers pass at least one missing value. */
export function firstReason(...ms: Maybe<unknown>[]): Reason {
  for (const m of ms) {
    if ("reason" in m) {
      return m.reason;
    }
  }
  return "not_applicable";
}

/** finite returns v when it is a finite number, and a missing value with r otherwise. */
export function finite(v: number, r: Reason): MaybeNumber {
  return Number.isFinite(v) ? some(v) : none(r);
}

/** Tone compares a value with a target. Financial changes that are neither good nor bad are neutral. */
export type Tone = "good" | "warn" | "bad" | "neutral";

/** Axis is a chart scale with tick steps. Min and max are tick positions. */
export interface Axis {
  min: number;
  max: number;
  step: number;
}

/** Range is a chart scale without ticks. */
export interface Range {
  min: number;
  max: number;
}

/** widen gives a zero-width domain a visible width. */
function widen(lo: number, hi: number): [number, number] {
  if (hi > lo) {
    return [lo, hi];
  }
  const d = Math.abs(hi) > 0 ? Math.abs(hi) * 0.1 : 1;
  return [lo - d, hi + d];
}

export function newAxis(lo: number, hi: number, step: number): Axis {
  const [min, max] = widen(lo, hi);
  return { min, max, step };
}

export function newRange(lo: number, hi: number): Range {
  const [min, max] = widen(lo, hi);
  return { min, max };
}

/** roundHalf rounds to the nearest 0.5, the precision of every P/E lever. */
export function roundHalf(x: number): number {
  return Math.round(x * 2) / 2;
}

/**
 * toFixed rounds like Go's strconv formatting with digits decimals. Both
 * round the exact binary value; they differ only on exact decimal ties,
 * which the engine's grids never produce.
 */
export function toFixed(x: number, digits: number): number {
  return Number(x.toFixed(digits));
}

/** median returns the median of the finite values: the mean of the two middle values for an even count. */
export function median(values: readonly number[]): MaybeNumber {
  const v = values.filter((x) => Number.isFinite(x)).sort((a, b) => a - b);
  if (v.length === 0) {
    return none("insufficient_history");
  }
  const h = Math.floor(v.length / 2);
  return some(v.length % 2 === 1 ? at(v, h) : (at(v, h - 1) + at(v, h)) / 2);
}

/** at returns xs[i] and throws when i is out of range, which is a bug in the engine. */
export function at<T>(xs: readonly T[], i: number): T {
  const v = xs[i];
  if (v === undefined) {
    throw new RangeError(`engine: index ${String(i)} out of range 0..${String(xs.length - 1)}`);
  }
  return v;
}
