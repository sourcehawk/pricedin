import type { BaselineMode, ChartRange, ExitPeMode, ScenarioCase } from "@/engine";
import {
  defaultLevers,
  leversReducer,
  type LeverAction,
  type LeverState,
  type ViewValue,
} from "./levers";

/** storageKey is the localStorage key of the saved levers. A new shape gets a new version. */
export const storageKey = "pricedin.levers";
const version = 1;

// The browser can block or clear storage (private windows, site settings),
// so every access is a convenience that may fail.

/** saveLevers saves the state for this browser; it never throws. */
export function saveLevers(s: LeverState): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify({ version, state: s }));
  } catch {
    // Not saved: the page works the same without it.
  }
}

/**
 * loadLevers returns the saved state, or null when there is none or it
 * cannot be read. A field with a wrong type takes its default, and a value
 * outside the engine's bounds is clamped. It never throws.
 */
export function loadLevers(): LeverState | null {
  let raw: string | null;
  try {
    raw = localStorage.getItem(storageKey);
  } catch {
    return null;
  }
  if (raw === null) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(parsed) || parsed.version !== version || !isRecord(parsed.state)) {
    return null;
  }
  return fromStored(parsed.state);
}

type Rec = Record<string, unknown>;

function isRecord(v: unknown): v is Rec {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

const num = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const numOrNull = (v: unknown): number | null => (num(v) ? v : null);

const exitModes: readonly ExitPeMode[] = ["estimated", "today", "trailing", "custom"];
const baselineModes: readonly BaselineMode[] = ["weighted", "today", "trailing", "custom"];
const ranges: readonly ChartRange[] = ["6M", "1Y", "3Y"];

function oneOf<T extends string>(v: unknown, options: readonly T[]): v is T {
  return typeof v === "string" && (options as readonly string[]).includes(v);
}

function scenarioCases(v: unknown): ScenarioCase[] | null {
  if (!Array.isArray(v)) {
    return null;
  }
  const cases = v.filter(
    (c): c is ScenarioCase =>
      isRecord(c) && typeof c.name === "string" && num(c.growth) && num(c.exitPe),
  );
  return cases.length === v.length ? cases : null;
}

function isViewValue(v: unknown): v is ViewValue {
  return (
    typeof v === "string" ||
    typeof v === "boolean" ||
    num(v) ||
    (Array.isArray(v) && v.every((x) => typeof x === "string"))
  );
}

function fromStored(r: Rec): LeverState {
  const actions: LeverAction[] = [];
  const push = (a: LeverAction | null): void => {
    if (a !== null) {
      actions.push(a);
    }
  };
  for (const field of ["price", "ttmEps", "dividend"] as const) {
    const v = numOrNull(r[field]);
    push(v === null ? null : { type: "setStartingPoint", field, value: v });
  }
  push(num(r.targetReturn) ? { type: "setTargetReturn", value: r.targetReturn } : null);
  push(num(r.holdingYears) ? { type: "setHoldingYears", value: r.holdingYears } : null);

  const e = isRecord(r.exitPe) && oneOf(r.exitPe.mode, exitModes) ? r.exitPe : null;
  if (e !== null) {
    push(num(e.custom) ? { type: "setExitPe", value: e.custom } : null);
    push({ type: "setExitPeMode", mode: e.mode as ExitPeMode });
    push(num(e.trailingMonths) ? { type: "setExitPeMonths", value: e.trailingMonths } : null);
  }

  const b = isRecord(r.baseline) && oneOf(r.baseline.mode, baselineModes) ? r.baseline : null;
  if (b !== null) {
    push(num(b.rate) ? { type: "setCustomRate", value: b.rate } : null);
    const rates = Array.isArray(b.rates) && b.rates.every(num) ? b.rates : null;
    if (rates !== null && rates.length > 0) {
      push({ type: "followTrajectory", rates });
      if (b.ratesSource === "user") {
        push({ type: "setYearRate", index: 0, value: rates[0] ?? 0, current: rates });
      }
    }
    push({ type: "setYearly", yearly: b.yearly === true });
    push({ type: "setBaselineMode", mode: b.mode as BaselineMode });
    push(num(b.trailingMonths) ? { type: "setBaselineMonths", value: b.trailingMonths } : null);
  }

  const cases = scenarioCases(r.scenarios);
  push(cases === null ? null : { type: "setScenarios", cases });
  push(oneOf(r.chartRange, ranges) ? { type: "setChartRange", range: r.chartRange } : null);
  if (isRecord(r.views)) {
    for (const [key, value] of Object.entries(r.views)) {
      push(isViewValue(value) ? { type: "setView", key, value } : null);
    }
  }

  const start: LeverState = {
    ...defaultLevers,
    ticker: typeof r.ticker === "string" && r.ticker !== "" ? r.ticker.toUpperCase() : null,
  };
  return actions.reduce(leversReducer, start);
}
