import {
  baselinePath,
  growthScale,
  trailing,
  trajectory,
  yearlyAverage,
  yearRates,
  type Trailing,
  type Trajectory,
} from "./baselines";
import {
  exitPeNeeded,
  growthInRange,
  meetsTarget,
  missingRequirement,
  project,
  requiredGrowth,
  requirement,
  tone,
  verdict,
  type RequirementResult,
  type Verdict,
} from "./core";
import { exitPeView, type ExitPeView } from "./exitpe";
import { financials, type FinancialsView } from "./financials";
import { returnGrid, type Grid } from "./grid";
import {
  minCustomExitPe,
  validateHistory,
  validateLevers,
  type BaselineMode,
  type History,
  type Investment,
  type Levers,
} from "./inputs";
import { earningsPaths, howDemanding, type Demanding, type Paths } from "./paths";
import { priceChart, shareCountEvents, type Chart, type ShareEvent } from "./pricechart";
import { defaultScenarios, scenarios, type ScenarioSet } from "./scenarios";
import { Series, type QuarterPoint } from "./series";
import {
  at,
  firstReason,
  none,
  some,
  type Axis,
  type Maybe,
  type MaybeNumber,
  type Tone,
} from "./value";

/** Evaluation is everything the page shows for one stock and one set of levers. */
export interface Evaluation {
  /** Price over TTM EPS (the levers). */
  peNow: MaybeNumber;
  exitPe: ExitPeView;
  baselines: Baselines;
  /** Only for the custom year-by-year baseline. */
  yearByYear: Maybe<YearByYear>;
  /** The trajectory fill of the weighted trend over the holding period, whatever the baseline. */
  trajectory: Maybe<Trajectory>;
  required: RequirementResult;
  /** Baseline minus required growth. */
  gap: MaybeNumber;
  /** Undemanding when the dividends alone reach the target. */
  verdict: Maybe<Verdict>;
  atBaseline: Maybe<AtBaseline>;
  sensitivity: Maybe<LeverSensitivity>;
  quarters: QuarterView[];
  /** The growth ruler's scale over the required growth and the baselines. */
  growthScale: Axis;
  returnGrid: Maybe<Grid>;
  earningsPaths: Maybe<Paths>;
  howDemanding: Demanding;
  scenarios: EvaluatedScenarios;
  shareCountEvents: ShareEvent[];
  priceChart: Maybe<Chart>;
  financials: FinancialsView;
}

/** Baselines are the growth baselines and the one in use. */
export interface Baselines {
  active: BaselineMode;
  /** The growth of the active baseline. */
  growth: MaybeNumber;
  weighted: MaybeNumber;
  today: MaybeNumber;
  trailing: Trailing;
  /** The flat custom rate or the compound average of the yearly rates; not_applicable unless the custom baseline is active. */
  custom: MaybeNumber;
  /** The latest quarter's YoY growth minus the YoY growth three quarters earlier: the direction of the last four readings. */
  yoyShift: MaybeNumber;
}

/** YearByYear summarizes the custom year-by-year rates. */
export interface YearByYear {
  /** One per year of the holding period. */
  rates: number[];
  compoundAverage: number;
  simpleAverage: number;
  /** EPS at the end of the holding period. */
  endEps: MaybeNumber;
  /** Set when the simple average differs from the compound one by 0.2 pp or more. */
  showSimpleAverage: boolean;
}

/** AtBaseline is the outcome if EPS grows at the baseline and the stock sells at the exit P/E in use. */
export interface AtBaseline {
  exitPrice: number;
  totalReturn: number;
  yearlyReturn: number;
  vsTarget: number;
  tone: Tone;
  meetsTarget: boolean;
  /** EPS at the exit. */
  eps: number;
  /** EPS minus the EPS needed. */
  epsGap: MaybeNumber;
  exitPeNeeded: MaybeNumber;
  /** The baseline is within 2 pp of the required growth or the exit P/E within 1.5 of the one needed: a small change flips the tone. */
  fragile: boolean;
}

/** LeverSensitivity is the required growth after one lever change each. */
export interface LeverSensitivity {
  /** Exit P/E 2 lower, at least 0.5 and never above the exit P/E in use. */
  lowerExitPe: SensitivityAt;
  /** Target 2 pp lower, at least 0. */
  lowerTarget: SensitivityAt;
  /** A 10% target, the S&P 500's long-run average. */
  marketTarget: SensitivityAt;
  oneYear: SensitivityAt;
  fiveYears: SensitivityAt;
}

/** SensitivityAt is the required growth with one lever changed. Only the changed lever is set. */
export interface SensitivityAt {
  exitPe?: number;
  targetReturn?: number;
  years?: number;
  requiredGrowth: MaybeNumber;
}

/** QuarterView is one quarter with its derived values. */
export interface QuarterView extends QuarterPoint {
  label: string;
  periodEnd: string;
  eps: number | null;
}

/** EvaluatedScenarios are the cases in use and their outcomes. */
export interface EvaluatedScenarios extends ScenarioSet {
  /** The cases are the defaults, not the user's. */
  defaults: boolean;
}

/** marketReturn is the S&P 500's long-run average yearly return. */
const marketReturn = 0.1;

/**
 * evaluate computes everything the page shows. It throws an
 * InvalidInputError when the levers or the history are invalid (see
 * validateLevers and validateHistory); values that cannot be computed from
 * valid inputs are missing with a reason instead.
 */
export function evaluate(h: History, l: Levers): Evaluation {
  validateLevers(l);
  validateHistory(h);
  const quarters = h.quarters ?? [];
  const daily = h.daily ?? [];
  const s = new Series(quarters);
  const n = l.holdingYears;
  const inv: Investment = {
    price: l.price,
    eps: l.ttmEps,
    targetReturn: l.targetReturn,
    years: n,
    dividend: l.dividend,
  };
  const peNow: MaybeNumber = l.ttmEps > 0 ? some(l.price / l.ttmEps) : none("needs_positive_eps");

  // The baselines.
  const weighted = s.weighted();
  const today = s.today();
  const trailingGrowth = trailing(s, l.baseline.trailingMonths ?? 0);
  let custom: MaybeNumber = none("not_applicable");
  let yearByYear: Maybe<YearByYear> = none("not_applicable");
  let yearly: number[] | null = null;
  if (l.baseline.mode === "custom") {
    const c = l.baseline.custom ?? { yearly: false, rate: 0, rates: null };
    custom = some(c.rate);
    if (c.yearly) {
      const rates = yearRates(c, n);
      yearly = rates;
      const avg = yearlyAverage(rates);
      custom = some(avg);
      let simple = 0;
      for (const r of rates) {
        simple += r;
      }
      simple /= rates.length;
      yearByYear = some({
        rates,
        compoundAverage: avg,
        simpleAverage: simple,
        endEps:
          l.ttmEps > 0
            ? some(baselinePath(l.ttmEps, avg, 4 * n, rates))
            : none("needs_positive_eps"),
        showSimpleAverage: Math.abs(simple - avg) >= 0.002,
      });
    }
  }
  let g: MaybeNumber;
  switch (l.baseline.mode) {
    case "today":
      g = today;
      break;
    case "trailing":
      g = trailingGrowth.growth;
      break;
    case "custom":
      g = custom;
      break;
    default:
      g = weighted;
  }
  let yoyShift: MaybeNumber = none("insufficient_history");
  if (s.last >= 3) {
    const cur = at(s.yoy, s.last);
    const old = at(s.yoy, s.last - 3);
    yoyShift =
      cur.value !== null && old.value !== null
        ? some(cur.value - old.value)
        : none(firstReason(cur, old));
  }
  const baselines: Baselines = {
    active: l.baseline.mode,
    growth: g,
    weighted,
    today,
    trailing: trailingGrowth,
    custom,
    yoyShift,
  };

  // The exit P/E and the requirement.
  const exitPe = exitPeView(s, l, peNow);
  const pe: MaybeNumber = exitPe.inUse.value === null ? exitPe.inUse : some(exitPe.inUse.value.pe);
  const required = pe.value === null ? missingRequirement(inv, pe) : requirement(inv, pe.value);
  const req = required.growth;
  let gap: MaybeNumber;
  let verdictOut: Maybe<Verdict>;
  if (req.value === null) {
    gap = req;
    verdictOut = req.reason === "dividends_reach_target" ? some("undemanding") : req;
  } else if (g.value === null) {
    gap = g;
    verdictOut = g;
  } else {
    const d = g.value - req.value;
    gap = some(d);
    verdictOut = some(verdict(d));
  }

  const ev: Evaluation = {
    peNow,
    exitPe,
    baselines,
    yearByYear,
    trajectory: trajectory(s, n),
    required,
    gap,
    verdict: verdictOut,
    atBaseline: atBaseline(inv, g, pe, req, required.eps),
    sensitivity: sensitivity(inv, pe),
    quarters: quarters.map((q, i) => ({
      label: q.label,
      periodEnd: q.periodEnd,
      eps: q.eps,
      ...s.point(i),
    })),
    growthScale: growthScale([
      req.value,
      today.value,
      weighted.value,
      trailingGrowth.growth.value,
      g.value,
    ]),
    returnGrid:
      pe.value === null
        ? none(l.ttmEps > 0 ? pe.reason : "needs_positive_eps")
        : returnGrid({
            ...inv,
            exitPe: pe.value,
            required: req,
            baseline: { mode: l.baseline.mode, growth: g },
            weighted,
            today,
            trailing: trailingGrowth.growth,
          }),
    earningsPaths: earningsPaths({
      quarters,
      eps: l.ttmEps,
      years: n,
      baseline: g,
      yearlyRates: yearly,
      requiredEps: required.eps,
    }),
    howDemanding: howDemanding(quarters, inv, pe),
    scenarios: evaluatedScenarios(inv, l, weighted, pe),
    shareCountEvents: shareCountEvents(quarters),
    priceChart: priceChart(daily, quarters, l.chartRange),
    financials: financials(quarters),
  };
  return ev;
}

function evaluatedScenarios(
  inv: Investment,
  l: Levers,
  weighted: MaybeNumber,
  pe: MaybeNumber,
): EvaluatedScenarios {
  const cases = l.scenarios ?? [];
  const defaults = cases.length === 0;
  const set = scenarios(inv, defaults ? defaultScenarios(weighted.value, pe.value) : cases);
  return { defaults, ...set };
}

function atBaseline(
  inv: Investment,
  g: MaybeNumber,
  pe: MaybeNumber,
  req: MaybeNumber,
  reqEps: MaybeNumber,
): Maybe<AtBaseline> {
  if (!(inv.eps > 0)) {
    return none("needs_positive_eps");
  }
  if (pe.value === null) {
    return pe;
  }
  if (g.value === null) {
    return g;
  }
  if (!growthInRange(g.value)) {
    return none("growth_out_of_range");
  }
  const gv = g.value;
  const p = project(inv, gv, pe.value);
  const need = exitPeNeeded(inv, gv);
  const eps = inv.eps * Math.pow(1 + gv, inv.years);
  const diff = p.yearlyReturn - inv.targetReturn;
  return some({
    exitPrice: p.exitPrice,
    totalReturn: p.totalReturn,
    yearlyReturn: p.yearlyReturn,
    vsTarget: diff,
    tone: tone(diff),
    meetsTarget: meetsTarget(p.yearlyReturn, inv.targetReturn),
    eps,
    epsGap: reqEps.value === null ? reqEps : some(eps - reqEps.value),
    exitPeNeeded: need,
    fragile:
      (req.value !== null && Math.abs(gv - req.value) < 0.02) ||
      (need.value !== null && Math.abs(need.value - pe.value) < 1.5),
  });
}

function sensitivity(inv: Investment, pe: MaybeNumber): Maybe<LeverSensitivity> {
  if (!(inv.eps > 0)) {
    return none("needs_positive_eps");
  }
  if (pe.value === null) {
    return pe;
  }
  const exitPe = pe.value;
  const lowerPe = Math.min(exitPe, Math.max(minCustomExitPe, exitPe - 2));
  const lowerTarget = Math.max(0, inv.targetReturn - 0.02);
  const withLever = (change: Partial<Investment>): MaybeNumber =>
    requiredGrowth({ ...inv, ...change }, exitPe);
  return some({
    lowerExitPe: { exitPe: lowerPe, requiredGrowth: requiredGrowth(inv, lowerPe) },
    lowerTarget: {
      targetReturn: lowerTarget,
      requiredGrowth: withLever({ targetReturn: lowerTarget }),
    },
    marketTarget: {
      targetReturn: marketReturn,
      requiredGrowth: withLever({ targetReturn: marketReturn }),
    },
    oneYear: { years: 1, requiredGrowth: withLever({ years: 1 }) },
    fiveYears: { years: 5, requiredGrowth: withLever({ years: 5 }) },
  });
}
