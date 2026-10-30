import { isDate } from "./dates";

/**
 * Quarter is one fiscal quarter of a stock's history. Per-share values and
 * the close are split-adjusted; amounts are US dollars. Null or absent was
 * not reported.
 */
export interface Quarter {
  /** "Q3 FY26"; the earnings paths name future quarters only after labels of this form. */
  label: string;
  /** YYYY-MM-DD. */
  periodEnd: string;
  /** Diluted EPS of the quarter. */
  eps?: number | null;
  /** Close on or before periodEnd. */
  close?: number | null;
  /** Weighted average, whole shares. */
  dilutedShares?: number | null;
  revenue?: number | null;
  operatingIncome?: number | null;
  operatingCosts?: number | null;
  freeCashFlow?: number | null;
}

/** PricePoint is one daily close with the TTM EPS that was known on that day; null or absent was not known. */
export interface PricePoint {
  /** YYYY-MM-DD. */
  date: string;
  close: number;
  ttmEps?: number | null;
}

/**
 * History is everything the engine knows about a stock: consecutive
 * quarters and trading days, oldest first. Null or absent is an empty list.
 */
export interface History {
  // Go marshals a nil slice as null.
  quarters?: readonly Quarter[] | null;
  daily?: readonly PricePoint[] | null;
}

/** QuarterRow is a Quarter with each field present: null where it was not reported. */
export type QuarterRow = { readonly [K in keyof Quarter]-?: Exclude<Quarter[K], undefined> };

/** DayRow is a PricePoint with a TTM EPS that is a number or null. */
export type DayRow = { readonly [K in keyof PricePoint]-?: Exclude<PricePoint[K], undefined> };

/** quarterRows returns the quarters with each absent field as null, as Go decodes them. */
export function quarterRows(quarters: readonly Quarter[] | null | undefined): QuarterRow[] {
  return (quarters ?? []).map((q) => ({
    label: q.label,
    periodEnd: q.periodEnd,
    eps: q.eps ?? null,
    close: q.close ?? null,
    dilutedShares: q.dilutedShares ?? null,
    revenue: q.revenue ?? null,
    operatingIncome: q.operatingIncome ?? null,
    operatingCosts: q.operatingCosts ?? null,
    freeCashFlow: q.freeCashFlow ?? null,
  }));
}

/** dayRows returns the days with an absent TTM EPS as null, as Go decodes them. */
export function dayRows(daily: readonly PricePoint[] | null | undefined): DayRow[] {
  return (daily ?? []).map((d) => ({ date: d.date, close: d.close, ttmEps: d.ttmEps ?? null }));
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
  rates?: readonly number[] | null;
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
  /** Yearly, per share, counted as cash and not reinvested. Absent is 0. */
  dividend?: number;
  targetReturn: number;
  /** Whole years, 1 to 10. */
  holdingYears: number;
  exitPe: ExitPeLever;
  baseline: BaselineLever;
  /** The user's bear, base and bull cases; empty, null or absent means the defaults around the weighted trend. */
  scenarios?: readonly ScenarioCase[] | null;
  /** Empty or absent means 3Y. */
  chartRange?: ChartRange | "";
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
/** maxPrice is the largest price or close, far above any listed share. */
export const maxPrice = 1e8;
const maxScenario = 1e30;
/** maxPerShare is the largest EPS or dividend per share. */
export const maxPerShare = 1e6;
/** minPerShare is the smallest price, or EPS other than zero, per share. */
export const minPerShare = 1e-4;
const maxAmount = 1e15;
const minAmount = 1e-6;

function fail(message: string): never {
  throw new InvalidInputError(message);
}

function validPrice(name: string, v: number): void {
  if (!Number.isFinite(v) || v < minPerShare || v > maxPrice) {
    fail(`${name} must be ${String(minPerShare)} to ${String(maxPrice)}, got ${String(v)}`);
  }
}

function validEps(name: string, v: number): void {
  const a = Math.abs(v);
  if (!Number.isFinite(v) || (v !== 0 && (a < minPerShare || a > maxPerShare))) {
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
 * evaluate cannot use. The price is 0.0001 to 100,000,000; EPS is zero or
 * 0.0001 to 1,000,000 in size; the dividend is 0 to 1,000,000. The target
 * return is 0 to 50%, the holding period 1 to 10 whole years, a custom exit
 * P/E 0.5 to 1000, custom rates -50% to 200%, and look-backs 0 (the default)
 * to 120 whole months. A case has growth and an exit P/E of at most 10^30 in
 * size. Zero or negative EPS is valid: it gives values with reason
 * needs_positive_eps.
 */
export function validateLevers(l: Levers): void {
  validPrice("price", l.price);
  validEps("ttmEps", l.ttmEps);
  const dividend = l.dividend ?? 0;
  if (!Number.isFinite(dividend) || dividend < 0 || dividend > maxPerShare) {
    fail(`dividend must be 0 to ${String(maxPerShare)}, got ${String(dividend)}`);
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
    case undefined:
    case "":
    case "6M":
    case "1Y":
    case "3Y":
      break;
    default:
      fail(`unknown chartRange ${JSON.stringify(l.chartRange)}`);
  }
}

/**
 * validateHistory throws an InvalidInputError for a history that evaluate
 * cannot use: an EPS or TTM EPS outside the bounds of the EPS lever, a
 * close outside the bounds of the price lever, a share count or dollar
 * amount that is not zero or 0.000001 to 10^15 in size, a date that is not
 * a YYYY-MM-DD day that exists, or a daily date that is not after the one
 * before it.
 */
export function validateHistory(h: History): void {
  validateRows(quarterRows(h.quarters), dayRows(h.daily));
}

/** validateRows is validateHistory for a history whose absent fields are null. */
export function validateRows(quarters: readonly QuarterRow[], daily: readonly DayRow[]): void {
  quarters.forEach((q, i) => {
    const at = `quarters[${String(i)}]`;
    if (!isDate(q.periodEnd)) {
      fail(`${at}.periodEnd must be a YYYY-MM-DD date, got ${JSON.stringify(q.periodEnd)}`);
    }
    if (q.eps !== null) {
      validEps(`${at}.eps`, q.eps);
    }
    if (q.close !== null) {
      validPrice(`${at}.close`, q.close);
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
  daily.forEach((d, i) => {
    const at = `daily[${String(i)}]`;
    if (!isDate(d.date)) {
      fail(`${at}.date must be a YYYY-MM-DD date, got ${JSON.stringify(d.date)}`);
    }
    validPrice(`${at}.close`, d.close);
    if (d.ttmEps !== null) {
      validEps(`${at}.ttmEps`, d.ttmEps);
    }
    if (prev !== undefined && !(prev < d.date)) {
      fail(`${at}.date ${d.date} is not after ${prev}`);
    }
    prev = d.date;
  });
}
