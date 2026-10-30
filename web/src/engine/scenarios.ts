import { growthInRange, meetsTarget, project, tone } from "./core";
import type { Investment, ScenarioCase } from "./inputs";
import { none, roundHalf, some, type Maybe, type MaybeNumber, type Tone } from "./value";

const fallbackGrowth = 0.05;
const fallbackExitPe = 20;

/**
 * defaultScenarios returns the default bear, base and bull cases: the
 * weighted trend 6 pp lower, as is, and 6 pp higher, at 0.75, 1 and 1.2
 * times the exit P/E. Growth is rounded to 0.5 pp and the exit P/E to 0.5;
 * the bear exit P/E is at least 6. Null inputs use 5% growth and 20x.
 */
export function defaultScenarios(weighted: number | null, exitPe: number | null): ScenarioCase[] {
  const g = weighted !== null && Number.isFinite(weighted) ? weighted : fallbackGrowth;
  const pe = exitPe !== null && Number.isFinite(exitPe) ? exitPe : fallbackExitPe;
  // Rounded in percent, as the case editors show them.
  const pctHalf = (v: number): number => Math.round(v * 2) / 2 / 100;
  return [
    { name: "Bear", growth: pctHalf((g - 0.06) * 100), exitPe: roundHalf(Math.max(6, pe * 0.75)) },
    { name: "Base", growth: pctHalf(g * 100), exitPe: roundHalf(pe) },
    { name: "Bull", growth: pctHalf((g + 0.06) * 100), exitPe: roundHalf(pe * 1.2) },
  ];
}

/** ScenarioSet is the outcome of the bear, base and bull cases. */
export interface ScenarioSet {
  cases: ScenarioOutcome[];
  /** The names of the cases whose yearly return reaches the target return, in case order. */
  meetingTarget: string[];
  worstYearlyReturn: MaybeNumber;
}

/** ScenarioOutcome is one case with its result. */
export interface ScenarioOutcome extends ScenarioCase {
  result: Maybe<ScenarioResult>;
}

/** ScenarioResult is the outcome of one case. */
export interface ScenarioResult {
  /** EPS at the exit. */
  eps: number;
  exitPrice: number;
  totalReturn: number;
  yearlyReturn: number;
  vsTarget: number;
  tone: Tone;
  meetsTarget: boolean;
}

/**
 * scenarios returns the outcome of each case. A result is missing when EPS
 * or the case's exit P/E is not positive, or when its growth is -100% or
 * lower or above 1000%; worstYearlyReturn skips missing results.
 */
export function scenarios(inv: Investment, cases: readonly ScenarioCase[]): ScenarioSet {
  const meetingTarget: string[] = [];
  let worst = Infinity;
  const outcomes = cases.map((c): ScenarioOutcome => {
    const base = { name: c.name, growth: c.growth, exitPe: c.exitPe };
    if (!(inv.eps > 0)) {
      return { ...base, result: none("needs_positive_eps") };
    }
    if (!growthInRange(c.growth)) {
      return { ...base, result: none("growth_out_of_range") };
    }
    if (!(c.exitPe > 0)) {
      return { ...base, result: none("needs_positive_eps") };
    }
    const p = project(inv, c.growth, c.exitPe);
    const diff = p.yearlyReturn - inv.targetReturn;
    const r: ScenarioResult = {
      eps: inv.eps * Math.pow(1 + c.growth, inv.years),
      exitPrice: p.exitPrice,
      totalReturn: p.totalReturn,
      yearlyReturn: p.yearlyReturn,
      vsTarget: diff,
      tone: tone(diff),
      meetsTarget: meetsTarget(p.yearlyReturn, inv.targetReturn),
    };
    if (r.meetsTarget) {
      meetingTarget.push(c.name);
    }
    worst = Math.min(worst, r.yearlyReturn);
    return { ...base, result: some(r) };
  });
  let worstYearlyReturn: MaybeNumber;
  const first = outcomes[0];
  if (worst !== Infinity) {
    worstYearlyReturn = some(worst);
  } else if (first !== undefined) {
    worstYearlyReturn = first.result.value === null ? first.result : none("not_applicable");
  } else if (!(inv.eps > 0)) {
    worstYearlyReturn = none("needs_positive_eps");
  } else {
    worstYearlyReturn = none("insufficient_history");
  }
  return { cases: outcomes, meetingTarget, worstYearlyReturn };
}
