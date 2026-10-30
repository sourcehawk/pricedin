/**
 * Quarter is one fiscal quarter of a stock's history. Per-share values and
 * the close are split-adjusted; amounts are US dollars. Null was not
 * reported.
 */
export interface Quarter {
  /** "Q3 FY26"; the earnings paths name future quarters only after labels of this form. */
  label: string;
  /** YYYY-MM-DD. */
  periodEnd: string;
  /** Diluted EPS of the quarter. */
  eps: number | null;
  /** Close on or before periodEnd. */
  close: number | null;
  /** Weighted average, whole shares. */
  dilutedShares: number | null;
  revenue: number | null;
  operatingIncome: number | null;
  operatingCosts: number | null;
  freeCashFlow: number | null;
}

/** PricePoint is one daily close with the TTM EPS that was known on that day. */
export interface PricePoint {
  /** YYYY-MM-DD. */
  date: string;
  close: number;
  ttmEps: number | null;
}

/** History is everything the engine knows about a stock: consecutive quarters and trading days, oldest first. */
export interface History {
  quarters: readonly Quarter[];
  daily: readonly PricePoint[];
}

/** ExitPeMode selects how the exit P/E is set. */
export type ExitPeMode = "estimated" | "today" | "trailing" | "custom";

/** ExitPeLever is the exit P/E lever. */
export interface ExitPeLever {
  mode: ExitPeMode;
  /** The exit P/E of the custom mode, 0.5 to 1000. */
  custom?: number;
  /** The look-back of the typical P/E in every mode; absent or 0 means 36. */
  trailingMonths?: number;
}

/** BaselineMode selects the growth baseline. */
export type BaselineMode = "weighted" | "today" | "trailing" | "custom";

/** CustomGrowth is the user's own growth: one rate for every year, or, with yearly, one rate per year. */
export interface CustomGrowth {
  yearly: boolean;
  rate: number;
  /** Years without a rate repeat the last rate, or rate when there is none. */
  rates: readonly number[] | null;
}

/** BaselineLever is the growth-baseline lever. */
export interface BaselineLever {
  mode: BaselineMode;
  /** The look-back of trailing growth in every mode; absent or 0 means 12. */
  trailingMonths?: number;
  /** Absent means a flat rate of 0. */
  custom?: CustomGrowth;
}

/** ScenarioCase is one of the bear, base and bull cases. */
export interface ScenarioCase {
  name: string;
  growth: number;
  exitPe: number;
}

/** ChartRange is the visible period of the price chart. */
export type ChartRange = "6M" | "1Y" | "3Y";

/** Levers are the user's inputs. Rates and returns are fractions (0.15). */
export interface Levers {
  price: number;
  ttmEps: number;
  /** Yearly, per share, counted as cash and not reinvested. */
  dividend: number;
  targetReturn: number;
  /** Whole years, 1 to 10. */
  holdingYears: number;
  exitPe: ExitPeLever;
  baseline: BaselineLever;
  /** The user's bear, base and bull cases; empty or null means the defaults around the weighted trend. */
  scenarios: readonly ScenarioCase[] | null;
  /** Empty means 3Y. */
  chartRange: ChartRange | "";
}

/** Investment is a purchase at price, held for years, that pays dividend per share each year in cash. */
export interface Investment {
  price: number;
  /** TTM EPS at purchase. */
  eps: number;
  targetReturn: number;
  years: number;
  dividend: number;
}

/** InvalidInputError is what evaluate throws for levers or a history it cannot use (Go's ErrInvalidInput). */
export class InvalidInputError extends Error {
  constructor(message: string) {
    super(`invalid engine input: ${message}`);
    this.name = "InvalidInputError";
  }
}

// The bounds of the levers and of the history, the same as the Go engine's.
// Within them no computed value overflows. The UI clamps the levers to them.
export const maxTargetReturn = 0.5;
export const minHoldingYears = 1;
export const maxHoldingYears = 10;
export const minCustomGrowth = -0.5;
export const maxCustomGrowth = 2;
export const minCustomExitPe = 0.5;
export const maxCustomExitPe = 1000;
/** maxMonths is the longest look-back of the typical P/E and of trailing growth. */
export const maxMonths = 120;
const maxScenario = 1e30;
const maxPerShare = 1e6;
const minPerShare = 1e-4;
const maxAmount = 1e15;
const minAmount = 1e-6;

function fail(message: string): never {
  throw new InvalidInputError(message);
}

function validPerShare(name: string, v: number, signed: boolean): void {
  const a = Math.abs(v);
  if (!Number.isFinite(v)) {
    fail(`${name} must be finite, got ${String(v)}`);
  }
  if (!signed && v <= 0) {
    fail(`${name} must be positive, got ${String(v)}`);
  }
  if (v !== 0 && (a < minPerShare || a > maxPerShare)) {
    fail(
      `${name} must be zero or ${String(minPerShare)} to ${String(maxPerShare)} in size, got ${String(v)}`,
    );
  }
}

function validAmount(name: string, v: number): void {
  const a = Math.abs(v);
  if (!Number.isFinite(v) || (v !== 0 && (a < minAmount || a > maxAmount))) {
    fail(
      `${name} must be zero or ${String(minAmount)} to ${String(maxAmount)} in size, got ${String(v)}`,
    );
  }
}

function validCustomRate(name: string, g: number): void {
  if (!Number.isFinite(g) || g < minCustomGrowth || g > maxCustomGrowth) {
    fail(
      `${name} must be ${String(minCustomGrowth)} to ${String(maxCustomGrowth)}, got ${String(g)}`,
    );
  }
}

function validMonths(name: string, m: number | undefined): void {
  if (m !== undefined && (!Number.isInteger(m) || m < 0 || m > maxMonths)) {
    fail(`${name} must be a whole number from 0 to ${String(maxMonths)}, got ${String(m)}`);
  }
}

/**
 * validateLevers throws an InvalidInputError for the first lever that
 * evaluate cannot use. The price is 0.0001 to 1,000,000; EPS is zero or
 * 0.0001 to 1,000,000 in size; the dividend is 0 to 1,000,000. The target
 * return is 0 to 50%, the holding period 1 to 10 whole years, a custom exit
 * P/E 0.5 to 1000, custom rates -50% to 200%, and look-backs 0 (the default)
 * to 120 whole months. A case has growth and an exit P/E of at most 10^30 in
 * size. Zero or negative EPS is valid: it gives values with reason
 * needs_positive_eps.
 */
export function validateLevers(l: Levers): void {
  validPerShare("price", l.price, false);
  validPerShare("ttmEps", l.ttmEps, true);
  if (!Number.isFinite(l.dividend) || l.dividend < 0 || l.dividend > maxPerShare) {
    fail(`dividend must be 0 to ${String(maxPerShare)}, got ${String(l.dividend)}`);
  }
  if (!Number.isFinite(l.targetReturn) || l.targetReturn < 0 || l.targetReturn > maxTargetReturn) {
    fail(`targetReturn must be 0 to ${String(maxTargetReturn)}, got ${String(l.targetReturn)}`);
  }
  if (
    !Number.isInteger(l.holdingYears) ||
    l.holdingYears < minHoldingYears ||
    l.holdingYears > maxHoldingYears
  ) {
    fail(
      `holdingYears must be a whole number from ${String(minHoldingYears)} to ${String(maxHoldingYears)}, got ${String(l.holdingYears)}`,
    );
  }
  switch (l.exitPe.mode) {
    case "estimated":
    case "today":
    case "trailing":
      break;
    case "custom": {
      const c = l.exitPe.custom ?? 0;
      if (!Number.isFinite(c) || c < minCustomExitPe || c > maxCustomExitPe) {
        fail(
          `exitPe.custom must be ${String(minCustomExitPe)} to ${String(maxCustomExitPe)}, got ${String(c)}`,
        );
      }
      break;
    }
    default:
      fail(`unknown exitPe.mode ${JSON.stringify(l.exitPe.mode)}`);
  }
  validMonths("exitPe.trailingMonths", l.exitPe.trailingMonths);
  validMonths("baseline.trailingMonths", l.baseline.trailingMonths);
  switch (l.baseline.mode) {
    case "weighted":
    case "today":
    case "trailing":
    case "custom":
      break;
    default:
      fail(`unknown baseline.mode ${JSON.stringify(l.baseline.mode)}`);
  }
  const custom = l.baseline.custom;
  if (custom !== undefined) {
    validCustomRate("baseline.custom.rate", custom.rate);
    (custom.rates ?? []).forEach((r, i) => {
      validCustomRate(`baseline.custom.rates[${String(i)}]`, r);
    });
  }
  (l.scenarios ?? []).forEach((c, i) => {
    if (!Number.isFinite(c.growth) || Math.abs(c.growth) > maxScenario) {
      fail(
        `scenarios[${String(i)}].growth must be at most ${String(maxScenario)} in size, got ${String(c.growth)}`,
      );
    }
    if (!Number.isFinite(c.exitPe) || Math.abs(c.exitPe) > maxScenario) {
      fail(
        `scenarios[${String(i)}].exitPe must be at most ${String(maxScenario)} in size, got ${String(c.exitPe)}`,
      );
    }
  });
  switch (l.chartRange) {
    case "":
    case "6M":
    case "1Y":
    case "3Y":
      break;
    default:
      fail(`unknown chartRange ${JSON.stringify(l.chartRange)}`);
  }
}

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

/**
 * validateHistory throws an InvalidInputError for a history that evaluate
 * cannot use: an EPS, close or TTM EPS outside the per-share bounds of
 * validateLevers, a close that is not positive, a share count or dollar
 * amount that is not zero or 0.000001 to 10^15 in size, a date that is not
 * YYYY-MM-DD, or a daily date that is not after the one before it.
 */
export function validateHistory(h: History): void {
  h.quarters.forEach((q, i) => {
    const at = `quarters[${String(i)}]`;
    if (!datePattern.test(q.periodEnd)) {
      fail(`${at}.periodEnd must be YYYY-MM-DD, got ${JSON.stringify(q.periodEnd)}`);
    }
    if (q.eps !== null) {
      validPerShare(`${at}.eps`, q.eps, true);
    }
    if (q.close !== null) {
      validPerShare(`${at}.close`, q.close, false);
    }
    const amounts = {
      dilutedShares: q.dilutedShares,
      revenue: q.revenue,
      operatingIncome: q.operatingIncome,
      operatingCosts: q.operatingCosts,
      freeCashFlow: q.freeCashFlow,
    };
    for (const [name, v] of Object.entries(amounts)) {
      if (v !== null) {
        validAmount(`${at}.${name}`, v);
      }
    }
  });
  let prev: string | undefined;
  h.daily.forEach((d, i) => {
    const at = `daily[${String(i)}]`;
    if (!datePattern.test(d.date)) {
      fail(`${at}.date must be YYYY-MM-DD, got ${JSON.stringify(d.date)}`);
    }
    validPerShare(`${at}.close`, d.close, false);
    if (d.ttmEps !== null) {
      validPerShare(`${at}.ttmEps`, d.ttmEps, true);
    }
    if (prev !== undefined && !(prev < d.date)) {
      fail(`${at}.date ${d.date} is not after ${prev}`);
    }
    prev = d.date;
  });
}
