import type { Quarter } from "./inputs";
import { at, none, some, type MaybeNumber } from "./value";

/** QuarterPoint holds the derived values of one quarter. */
export interface QuarterPoint {
  /** The last four quarters' EPS added up. */
  ttmEps: MaybeNumber;
  /** EPS against the same quarter a year earlier. */
  yoy: MaybeNumber;
  /** TTM EPS against four quarters earlier. */
  ttmYoy: MaybeNumber;
  /** The weighted trend as it stood at this quarter. */
  weightedTrend: MaybeNumber;
}

/** Series holds the derived quarterly values that most functions need. */
export class Series {
  /** Index of the latest quarter; -1 without quarters. */
  readonly last: number;
  readonly ttm: MaybeNumber[];
  readonly yoy: MaybeNumber[];
  readonly ttmYoy: MaybeNumber[];
  readonly wtd: MaybeNumber[];

  constructor(readonly q: readonly Quarter[]) {
    const n = q.length;
    this.last = n - 1;
    this.ttm = new Array<MaybeNumber>(n);
    this.yoy = new Array<MaybeNumber>(n);
    this.ttmYoy = new Array<MaybeNumber>(n);
    this.wtd = new Array<MaybeNumber>(n);
    for (let i = 0; i < n; i++) {
      this.ttm[i] = this.ttmAt(i);
      this.yoy[i] = this.yoyAt(i);
    }
    for (let i = 0; i < n; i++) {
      this.ttmYoy[i] = this.ttmYoyAt(i);
      this.wtd[i] = this.weightedAt(i);
    }
  }

  private ttmAt(i: number): MaybeNumber {
    if (i < 3) {
      return none("insufficient_history");
    }
    let sum = 0;
    // Newest first, as the Go engine adds them: floating-point addition is
    // not associative.
    for (let k = 0; k < 4; k++) {
      const e = at(this.q, i - k).eps;
      if (e === null) {
        return none("missing_data");
      }
      sum += e;
    }
    return some(sum);
  }

  private yoyAt(i: number): MaybeNumber {
    if (i < 4) {
      return none("insufficient_history");
    }
    const cur = at(this.q, i).eps;
    const base = at(this.q, i - 4).eps;
    if (cur === null || base === null) {
      return none("missing_data");
    }
    if (!(base > 0)) {
      return none("needs_positive_eps");
    }
    return some(cur / base - 1);
  }

  private ttmYoyAt(i: number): MaybeNumber {
    if (i < 7) {
      return none("insufficient_history");
    }
    const cur = at(this.ttm, i);
    const base = at(this.ttm, i - 4);
    if (cur.value === null) {
      return cur;
    }
    if (base.value === null) {
      return base;
    }
    if (!(base.value > 0)) {
      return none("needs_positive_eps");
    }
    return some(cur.value / base.value - 1);
  }

  /**
   * weightedAt is the weighted trend at quarter t: the average YoY growth of
   * the 8 quarters up to t, where a quarter's weight halves every 2 quarters
   * back. Missing quarters drop out of the average.
   */
  weightedAt(t: number): MaybeNumber {
    let sw = 0;
    let sv = 0;
    for (let k = 0; k < 8; k++) {
      const i = t - k;
      const y = i >= 0 && i < this.yoy.length ? at(this.yoy, i).value : null;
      if (y === null) {
        continue;
      }
      const w = Math.pow(0.5, k / 2);
      sw += w;
      sv += w * y;
    }
    if (sw === 0) {
      return none("insufficient_history");
    }
    return some(sv / sw);
  }

  /** weighted is the weighted trend today. */
  weighted(): MaybeNumber {
    return this.last < 0 ? none("insufficient_history") : at(this.wtd, this.last);
  }

  /** today is the YoY growth of the latest quarter. */
  today(): MaybeNumber {
    return this.last < 0 ? none("insufficient_history") : at(this.yoy, this.last);
  }

  /** point returns the derived values of quarter i. */
  point(i: number): QuarterPoint {
    return {
      ttmEps: at(this.ttm, i),
      yoy: at(this.yoy, i),
      ttmYoy: at(this.ttmYoy, i),
      weightedTrend: at(this.wtd, i),
    };
  }
}

/** quarterSeries returns the derived values of each quarter, in the order of quarters. */
export function quarterSeries(quarters: readonly Quarter[]): QuarterPoint[] {
  const s = new Series(quarters);
  return quarters.map((_, i) => s.point(i));
}

/** weightedTrend returns the weighted trend as it stood at quarter index at. */
export function weightedTrend(quarters: readonly Quarter[], index: number): MaybeNumber {
  if (index < 0 || index >= quarters.length) {
    return none("insufficient_history");
  }
  return at(new Series(quarters).wtd, index);
}
