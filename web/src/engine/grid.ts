import { growthInRange, project, tone } from "./core";
import type { BaselineMode, Investment } from "./inputs";
import {
  at,
  none,
  some,
  toFixed,
  type Maybe,
  type MaybeNumber,
  type Reason,
  type Tone,
} from "./value";

/** GridInput is what the return grid needs. */
export interface GridInput extends Investment {
  /** The exit P/E in use. */
  exitPe: number;
  required: MaybeNumber;
  baseline: ActiveBaseline;
  weighted: MaybeNumber;
  today: MaybeNumber;
  trailing: MaybeNumber;
}

/** ActiveBaseline is the baseline in use and its growth rate. */
export interface ActiveBaseline {
  mode: BaselineMode;
  growth: MaybeNumber;
}

/** GridTag marks the row of a growth rate. The custom tag appears only when the custom baseline is active. */
export type GridTag = "required" | "weighted" | "today" | "trailing" | "custom";

/** Grid is the return grid: rows are EPS growth rates, columns are exit P/Es. */
export interface Grid {
  /** Between the regular rows. */
  step: number;
  columns: GridColumn[];
  rows: GridRow[];
  /** The user's own cell: the exact baseline row at the exit P/E in use. Missing without a baseline. */
  you: Maybe<GridCell>;
  /** How much yearly return one row step adds in the exit P/E column in use, near the baseline. */
  columnSensitivity: Maybe<Sensitivity>;
}

/** GridColumn is one exit P/E. */
export interface GridColumn {
  exitPe: number;
  /** Within 0.5 of today's P/E. */
  today: boolean;
}

/** GridRow is one growth rate with its outcomes, one per column. */
export interface GridRow {
  growth: number;
  epsAtExit: number;
  /** The row of the exact baseline growth. */
  exact: boolean;
  /** The baselines whose value is nearest to this row. */
  tags: GridTag[];
  cells: GridOutcome[];
}

/** GridOutcome is one cell of the grid. */
export interface GridOutcome {
  exitPrice: number;
  totalReturn: number;
  yearlyReturn: number;
  /** Yearly return minus the target return. */
  vsTarget: number;
  tone: Tone;
}

/** GridCell is a position in the grid. */
export interface GridCell {
  row: number;
  column: number;
}

/** Sensitivity is the change in yearly return for one step of growth. */
export interface Sensitivity {
  growthStep: number;
  returnChange: number;
}

/**
 * returnGrid returns the return grid. The rows adapt to the growth rates of
 * the baselines and the requirement; the columns spread around the exit
 * P/E in use. A row at the exact baseline growth carries the baseline's tag
 * and the YOU cell; the other tags sit on their nearest row. The grid is
 * missing when EPS or the exit P/E is not positive.
 */
export function returnGrid(input: GridInput): Maybe<Grid> {
  if (!(input.eps > 0) || !(input.exitPe > 0)) {
    return none("needs_positive_eps");
  }
  const pe = input.exitPe;
  const g = input.baseline.growth;
  let lo = 0;
  let hi = 0.3;
  for (const m of [input.required, input.today, input.weighted, input.trailing, g]) {
    if (m.value !== null) {
      lo = Math.min(lo, m.value);
      hi = Math.max(hi, m.value);
    }
  }
  lo = Math.max(-0.4, lo);
  hi = Math.min(1.5, hi);
  let step = 0.2;
  for (const st of [0.025, 0.05, 0.1, 0.2]) {
    if (Math.ceil(hi / st) - Math.floor(lo / st) <= 13) {
      step = st;
      break;
    }
  }
  const regular: number[] = [];
  for (let i = Math.floor(lo / step); i <= Math.ceil(hi / step); i++) {
    regular.push(toFixed(i * step, 4));
  }

  const cols = gridColumns(pe);
  const youCol = cols.indexOf(pe);

  const gv = g.value !== null && growthInRange(g.value) ? g.value : null;
  const rows: GridRow[] = regular.map((v) => ({
    growth: v,
    epsAtExit: 0,
    exact: false,
    tags: [],
    cells: [],
  }));
  if (gv !== null) {
    const exact = rows.findIndex((r) => Math.abs(r.growth - gv) < 1e-9);
    if (exact >= 0) {
      at(rows, exact).exact = true;
    } else {
      const ins = rows.findIndex((r) => r.growth >= gv);
      rows.splice(ins < 0 ? rows.length : ins, 0, {
        growth: gv,
        epsAtExit: 0,
        exact: true,
        tags: [],
        cells: [],
      });
    }
  }
  const youRow = rows.findIndex((r) => r.exact);
  const growths = rows.map((r) => r.growth);
  const place = (tag: GridTag, active: boolean, v: MaybeNumber): void => {
    if (active) {
      if (youRow >= 0) {
        at(rows, youRow).tags.push(tag);
      }
      return;
    }
    if (v.value !== null) {
      at(rows, nearest(growths, v.value)).tags.push(tag);
    }
  };
  const mode = input.baseline.mode;
  place("required", false, input.required);
  place("weighted", mode === "weighted", input.weighted);
  place("today", mode === "today", input.today);
  place("trailing", mode === "trailing", input.trailing);
  if (mode === "custom") {
    place("custom", true, g);
  }

  const n = input.years;
  for (const row of rows) {
    row.epsAtExit = input.eps * Math.pow(1 + row.growth, n);
    row.cells = cols.map((c) => {
      const p = project(input, row.growth, c);
      const diff = p.yearlyReturn - input.targetReturn;
      return {
        exitPrice: p.exitPrice,
        totalReturn: p.totalReturn,
        yearlyReturn: p.yearlyReturn,
        vsTarget: diff,
        tone: tone(diff),
      };
    });
  }

  const nowPe = input.price / input.eps;
  const columns = cols.map((c) => ({ exitPe: c, today: Math.abs(c - nowPe) < 0.5 }));
  if (gv === null) {
    const reason: Reason = g.value === null ? g.reason : "growth_out_of_range";
    return some({ step, columns, rows, you: none(reason), columnSensitivity: none(reason) });
  }
  const yearlyAt = (ri: number): number =>
    project(input, at(regular, ri), at(cols, youCol)).yearlyReturn;
  const i = Math.min(Math.max(nearest(regular, gv), 1), regular.length - 1);
  return some({
    step,
    columns,
    rows,
    you: some({ row: youRow, column: youCol }),
    columnSensitivity: some({
      growthStep: at(regular, i) - at(regular, i - 1),
      returnChange: yearlyAt(i) - yearlyAt(i - 1),
    }),
  });
}

/** gridColumns spreads exit P/Es around pe: whole numbers below 40, steps of 5 above, and pe itself. */
function gridColumns(pe: number): number[] {
  const factors = [0.55, 0.7, 0.8, 0.9, 1, 1.12, 1.25, 1.4, 1.6];
  const cols: number[] = [];
  for (const f of factors) {
    let v = pe;
    if (f !== 1) {
      v = pe * f;
      v = v < 40 ? Math.round(v) : Math.round(v / 5) * 5;
    }
    // A neighbor of a tiny exit P/E can round to 0.
    if (v > 0 && !cols.includes(v)) {
      cols.push(v);
    }
  }
  return cols.sort((a, b) => a - b);
}

/** nearest returns the index of the value in vs nearest to v; the first one wins a tie. */
export function nearest(vs: readonly number[], v: number): number {
  let b = 0;
  vs.forEach((x, i) => {
    if (Math.abs(x - v) < Math.abs(at(vs, b) - v)) {
      b = i;
    }
  });
  return b;
}
