import {
  maxCustomExitPe,
  maxCustomGrowth,
  maxHoldingYears,
  maxMonths,
  maxTargetReturn,
  minCustomExitPe,
  minCustomGrowth,
  minHoldingYears,
  type BaselineMode,
  type ChartRange,
  type ExitPeMode,
  type Levers,
  type ScenarioCase,
} from "@/engine";

/** LeverKey names a lever of the left panel; walkthrough steps ring levers by these keys. */
export type LeverKey =
  "price" | "ttmEps" | "dividend" | "targetReturn" | "holdingYears" | "exitPe" | "baseline";

/** YearRatesSource says where the custom year-by-year rates came from. */
export type YearRatesSource = "trajectory" | "user";

/** ViewValue is the value of a panel view toggle, such as the return grid's view. */
export type ViewValue = string | number | boolean | readonly string[];

/**
 * LeverState is the state of every lever, in engine units: rates and
 * returns are fractions. It also holds the selected stock, the chart range
 * and the panel view toggles that persist with the levers.
 */
export interface LeverState {
  /** Upper case; null before the first pick. */
  ticker: string | null;
  /** The starting point the user typed; null takes the stock's value. */
  price: number | null;
  ttmEps: number | null;
  dividend: number | null;
  targetReturn: number;
  holdingYears: number;
  exitPe: {
    mode: ExitPeMode;
    /** The exit P/E of the custom mode. */
    custom: number;
    /** The look-back of the typical P/E, whatever the mode. */
    trailingMonths: number;
  };
  baseline: {
    mode: BaselineMode;
    /** The look-back of trailing growth, whatever the mode. */
    trailingMonths: number;
    /** The custom baseline uses one rate per year. */
    yearly: boolean;
    /** The flat custom rate. */
    rate: number;
    /** The year-by-year rates; may be shorter than the holding period. */
    rates: number[];
    ratesSource: YearRatesSource | null;
  };
  /** The bear, base and bull cases; null until the first evaluation after a pick or a reset pins them. */
  scenarios: ScenarioCase[] | null;
  chartRange: ChartRange;
  /** Panel view toggles by key, such as "grid.view". Each panel owns its keys. */
  views: Readonly<Record<string, ViewValue>>;
}

export const defaultLevers: LeverState = {
  ticker: null,
  price: null,
  ttmEps: null,
  dividend: null,
  targetReturn: 0.15,
  holdingYears: 3,
  exitPe: { mode: "estimated", custom: 20, trailingMonths: 36 },
  baseline: {
    mode: "weighted",
    trailingMonths: 12,
    yearly: false,
    rate: 0.12,
    rates: [],
    ratesSource: null,
  },
  scenarios: null,
  chartRange: "3Y",
  views: {},
};

/** StartingPointField is a field of the starting point. */
export type StartingPointField = "price" | "ttmEps" | "dividend";

/**
 * LeverAction changes the lever state. Values outside the engine's bounds
 * are clamped to them; values that are not finite numbers are ignored.
 */
export type LeverAction =
  /** A new stock resets every input except the target return and the holding period. */
  | { type: "pickStock"; ticker: string }
  /** Resets every input to its default and keeps the stock. */
  | { type: "reset" }
  /** null goes back to the stock's value. */
  | { type: "setStartingPoint"; field: StartingPointField; value: number | null }
  | { type: "setTargetReturn"; value: number }
  | { type: "setHoldingYears"; value: number }
  /** current is the exit P/E in use; the custom mode starts from it. */
  | { type: "setExitPeMode"; mode: ExitPeMode; current?: number | null }
  /** A typed or dragged exit P/E; switches to the custom mode. */
  | { type: "setExitPe"; value: number }
  | { type: "setExitPeMonths"; value: number }
  | { type: "setBaselineMode"; mode: BaselineMode }
  | { type: "setBaselineMonths"; value: number }
  /** A growth rate typed into the baseline's field; switches to a flat custom rate. */
  | { type: "setGrowth"; value: number }
  /** The flat custom rate from its slider. */
  | { type: "setCustomRate"; value: number }
  | { type: "setYearly"; yearly: boolean }
  /** current are the rates the page shows, one per year; the edited list becomes the user's own. */
  | { type: "setYearRate"; index: number; value: number; current: readonly number[] }
  /** Fills the year-by-year rates from the weighted trend's trajectory. */
  | { type: "followTrajectory"; rates: readonly number[] }
  /** Pins the cases of an evaluation when none are pinned; otherwise does nothing. */
  | { type: "pinScenarios"; cases: readonly ScenarioCase[] }
  | { type: "setScenarios"; cases: readonly ScenarioCase[] }
  | { type: "setChartRange"; range: ChartRange }
  | { type: "setView"; key: string; value: ViewValue }
  /** Replaces the state, such as one read from storage. */
  | { type: "restore"; state: LeverState };

const maxPerShare = 1e6;
const minPerShare = 1e-4;

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

const clampRate = (v: number): number => clamp(v, minCustomGrowth, maxCustomGrowth);
const clampMonths = (v: number): number => clamp(Math.round(v), 1, maxMonths);

function startingPoint(field: StartingPointField, v: number): number | undefined {
  switch (field) {
    case "price":
      return v >= minPerShare && v <= maxPerShare ? v : undefined;
    case "ttmEps": {
      const a = Math.abs(v);
      return v === 0 || (a >= minPerShare && a <= maxPerShare) ? v : undefined;
    }
    case "dividend":
      return v > maxPerShare ? undefined : Math.max(0, v);
  }
}

function numeric(a: LeverAction): number | null {
  return "value" in a && typeof a.value === "number" ? a.value : null;
}

/** leversReducer applies one action. */
export function leversReducer(s: LeverState, a: LeverAction): LeverState {
  const v = numeric(a);
  if (v !== null && !Number.isFinite(v)) {
    return s;
  }
  switch (a.type) {
    case "pickStock":
      return {
        ...defaultLevers,
        ticker: a.ticker.toUpperCase(),
        targetReturn: s.targetReturn,
        holdingYears: s.holdingYears,
        chartRange: s.chartRange,
        views: s.views,
      };
    case "reset":
      return { ...defaultLevers, ticker: s.ticker };
    case "setStartingPoint": {
      if (a.value === null) {
        return { ...s, [a.field]: null };
      }
      const value = startingPoint(a.field, a.value);
      return value === undefined ? s : { ...s, [a.field]: value };
    }
    case "setTargetReturn":
      return { ...s, targetReturn: clamp(a.value, 0, maxTargetReturn) };
    case "setHoldingYears":
      return { ...s, holdingYears: clamp(Math.round(a.value), minHoldingYears, maxHoldingYears) };
    case "setExitPeMode": {
      const current = a.current ?? null;
      const custom =
        a.mode === "custom" && current !== null && Number.isFinite(current)
          ? clamp(current, minCustomExitPe, maxCustomExitPe)
          : s.exitPe.custom;
      return { ...s, exitPe: { ...s.exitPe, mode: a.mode, custom } };
    }
    case "setExitPe":
      return {
        ...s,
        exitPe: {
          ...s.exitPe,
          mode: "custom",
          custom: clamp(a.value, minCustomExitPe, maxCustomExitPe),
        },
      };
    case "setExitPeMonths":
      return { ...s, exitPe: { ...s.exitPe, trailingMonths: clampMonths(a.value) } };
    case "setBaselineMode":
      return { ...s, baseline: { ...s.baseline, mode: a.mode } };
    case "setBaselineMonths":
      return { ...s, baseline: { ...s.baseline, trailingMonths: clampMonths(a.value) } };
    case "setGrowth":
      return {
        ...s,
        baseline: { ...s.baseline, mode: "custom", yearly: false, rate: clampRate(a.value) },
      };
    case "setCustomRate":
      return { ...s, baseline: { ...s.baseline, rate: clampRate(a.value) } };
    case "setYearly":
      return { ...s, baseline: { ...s.baseline, yearly: a.yearly } };
    case "setYearRate": {
      if (!Number.isInteger(a.index) || a.index < 0 || a.index >= a.current.length) {
        return s;
      }
      const rates = a.current.map((r, i) => (i === a.index ? clampRate(a.value) : r));
      return { ...s, baseline: { ...s.baseline, rates, ratesSource: "user" } };
    }
    case "followTrajectory":
      return {
        ...s,
        baseline: {
          ...s.baseline,
          mode: "custom",
          yearly: true,
          rates: a.rates.map(clampRate),
          ratesSource: "trajectory",
        },
      };
    case "pinScenarios":
      return s.scenarios === null ? { ...s, scenarios: a.cases.map((c) => ({ ...c })) } : s;
    case "setScenarios":
      return { ...s, scenarios: a.cases.map((c) => ({ ...c })) };
    case "setChartRange":
      return { ...s, chartRange: a.range };
    case "setView":
      return { ...s, views: { ...s.views, [a.key]: a.value } };
    case "restore":
      return a.state;
  }
}

/** StockStartingPoint is the starting point the stock's data gives; null where it has none. */
export interface StockStartingPoint {
  price: number | null;
  ttmEps: number | null;
  dividend: number | null;
}

/**
 * engineLevers returns the engine's levers: the user's starting point where
 * typed, else the stock's. It is null when neither gives a price or a TTM
 * EPS. trajectoryRates, the trajectory over the holding period, extends
 * year-by-year rates that came from the trajectory to a longer holding
 * period; the user's own rates repeat their last rate instead.
 */
export function engineLevers(
  s: LeverState,
  stock: StockStartingPoint,
  trajectoryRates: readonly number[] | null,
): Levers | null {
  const price = s.price ?? stock.price;
  const ttmEps = s.ttmEps ?? stock.ttmEps;
  if (price === null || ttmEps === null) {
    return null;
  }
  let rates = s.baseline.rates;
  if (
    s.baseline.ratesSource === "trajectory" &&
    trajectoryRates !== null &&
    rates.length < s.holdingYears
  ) {
    rates = [...rates, ...trajectoryRates.slice(rates.length, s.holdingYears)];
  }
  return {
    price,
    ttmEps,
    dividend: s.dividend ?? stock.dividend ?? 0,
    targetReturn: s.targetReturn,
    holdingYears: s.holdingYears,
    exitPe: {
      mode: s.exitPe.mode,
      custom: s.exitPe.custom,
      trailingMonths: s.exitPe.trailingMonths,
    },
    baseline: {
      mode: s.baseline.mode,
      trailingMonths: s.baseline.trailingMonths,
      custom: { yearly: s.baseline.yearly, rate: s.baseline.rate, rates },
    },
    scenarios: s.scenarios ?? [],
    chartRange: s.chartRange,
  };
}
