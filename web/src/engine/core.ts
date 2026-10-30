import type { Investment } from "./inputs";
import { finite, none, some, type Missing, type MaybeNumber, type Tone } from "./value";

/** grown returns (1+target)^years: what each dollar must become. */
function grown(inv: Investment): number {
  return Math.pow(1 + inv.targetReturn, inv.years);
}

/** neededFromExit is the part of the target value that the exit price must deliver. */
function neededFromExit(inv: Investment): number {
  return inv.price * grown(inv) - inv.dividend * inv.years;
}

/**
 * requiredGrowth returns the average yearly EPS growth (compound) that turns
 * the price into the target return when the stock sells at exitPe. It is
 * missing with needs_positive_eps when EPS or exitPe is not positive, and
 * with dividends_reach_target when the dividends alone reach the target.
 * The result can be negative.
 */
export function requiredGrowth(inv: Investment, exitPe: number): MaybeNumber {
  if (!(inv.eps > 0) || !(exitPe > 0)) {
    return none("needs_positive_eps");
  }
  const t = neededFromExit(inv);
  if (t <= 0) {
    return none("dividends_reach_target");
  }
  return finite(Math.pow(t / (inv.eps * exitPe), 1 / inv.years) - 1, "needs_positive_eps");
}

/** RequirementResult is what the target return requires at an exit P/E. */
export interface RequirementResult {
  growth: MaybeNumber;
  /** Share price needed at the exit. */
  exitPrice: MaybeNumber;
  /** exitPrice against today's price. */
  priceChange: MaybeNumber;
  /** EPS needed at the exit. */
  eps: MaybeNumber;
  /** Dividends received over the hold. */
  dividends: number;
  /** The baseline growth where the verdict turns tight. */
  tightFrom: MaybeNumber;
  /** The baseline growth where the verdict turns undemanding. */
  undemandingFrom: MaybeNumber;
}

/** missingRequirement is a requirement whose values all share one reason. */
export function missingRequirement(inv: Investment, growth: Missing): RequirementResult {
  return {
    growth,
    exitPrice: growth,
    priceChange: growth,
    eps: growth,
    dividends: inv.dividend * inv.years,
    tightFrom: growth,
    undemandingFrom: growth,
  };
}

/**
 * requirement returns the required growth with the exit price and EPS it
 * implies. All values share the reason of a missing required growth.
 */
export function requirement(inv: Investment, exitPe: number): RequirementResult {
  const g = requiredGrowth(inv, exitPe);
  if (g.value === null) {
    return missingRequirement(inv, g);
  }
  const r = g.value;
  // Not E*(1+r)^n: r rounds to -1 at a huge exit P/E, and that would lose
  // the price that the exit must deliver.
  const price = neededFromExit(inv);
  return {
    growth: g,
    exitPrice: some(price),
    priceChange: some(price / inv.price - 1),
    eps: some(price / exitPe),
    dividends: inv.dividend * inv.years,
    tightFrom: some(r - toneBand),
    undemandingFrom: some(r + toneBand),
  };
}

/** Projection is the outcome of growing EPS at a fixed rate and selling at an exit P/E. */
export interface Projection {
  exitPrice: number;
  /** Exit price plus dividends, against the price. */
  totalReturn: number;
  /** Compound; -1 when nothing is left. */
  yearlyReturn: number;
}

/** project returns the outcome of EPS growth g for the investment, selling at exitPe. It ignores the target return. */
export function project(inv: Investment, g: number, exitPe: number): Projection {
  const n = inv.years;
  const px = inv.eps * Math.pow(1 + g, n) * exitPe;
  const tot = px + inv.dividend * n;
  return {
    exitPrice: px,
    totalReturn: tot / inv.price - 1,
    yearlyReturn: Math.pow(Math.max(tot, 0) / inv.price, 1 / n) - 1,
  };
}

/**
 * exitPeNeeded returns the exit P/E at which EPS growth g reaches the target
 * return. It is missing with needs_positive_eps when EPS is not positive,
 * growth_out_of_range when g is -100% or lower or above 1000%, and
 * dividends_reach_target when any exit P/E would do.
 */
export function exitPeNeeded(inv: Investment, g: number): MaybeNumber {
  if (!(inv.eps > 0)) {
    return none("needs_positive_eps");
  }
  if (!growthInRange(g)) {
    return none("growth_out_of_range");
  }
  const t = neededFromExit(inv);
  if (!(t > 0)) {
    return none("dividends_reach_target");
  }
  return some(t / (inv.eps * Math.pow(1 + g, inv.years)));
}

/** maxGrowth is the highest growth rate that the engine compounds: 1000% a year. */
const maxGrowth = 10;

/** growthInRange reports whether the engine can compound g: above -100% and at most 1000% a year. */
export function growthInRange(g: number): boolean {
  return g > -1 && g <= maxGrowth;
}

/**
 * meetsTarget reports whether a yearly return reaches the target return. A
 * return within 1e-9 below it counts: a projection that lands exactly on
 * the target can come out a rounding error short.
 */
export function meetsTarget(yearly: number, target: number): boolean {
  return yearly >= target - 1e-9;
}

/** toneBand is the distance from a target that still counts as close to it. */
export const toneBand = 0.015;

/**
 * tone classifies a difference from a target (Go's ToneOf): good above
 * +1.5 pp, bad below -1.5 pp, warn within. A difference within 1e-9 of a
 * cutoff counts as within.
 */
export function tone(diff: number): Tone {
  if (diff > toneBand + 1e-9) {
    return "good";
  }
  if (diff < -toneBand - 1e-9) {
    return "bad";
  }
  return "warn";
}

/** Verdict says how demanding the price is against the growth baseline. */
export type Verdict = "undemanding" | "tight" | "demanding";

/** verdict classifies the gap between the baseline and the required growth (Go's VerdictOf). */
export function verdict(gap: number): Verdict {
  switch (tone(gap)) {
    case "good":
      return "undemanding";
    case "bad":
      return "demanding";
    default:
      return "tight";
  }
}

/** niceStep returns a tick step of 1, 2 or 5 times a power of ten that cuts rng into about target parts; 1 when rng is not positive. */
export function niceStep(rng: number, target: number): number {
  if (!(rng > 0)) {
    return 1;
  }
  const raw = rng / target;
  const m = Math.pow(10, Math.floor(Math.log10(raw)));
  const f = raw / m;
  if (f < 1.5) {
    return m;
  }
  if (f < 3) {
    return 2 * m;
  }
  if (f < 7) {
    return 5 * m;
  }
  return 10 * m;
}
