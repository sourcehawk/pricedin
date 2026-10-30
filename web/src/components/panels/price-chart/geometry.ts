// Drawing geometry of the price chart, in the units of its SVG view box.
// The values come from the engine; this module only places them.

import { bandPrice, type ChartRange, type Range } from "@/engine";

export const width = 760;
export const height = 320;
export const left = 50;
export const top = 12;
export const bottom = height - 26;

/** right is the right edge of the plot; the band labels need room beside it. */
export function right(bands: boolean): number {
  return width - (bands ? 50 : 14);
}

/** dayTime returns the UTC time of a YYYY-MM-DD date in milliseconds. */
export function dayTime(date: string): number {
  return Date.parse(`${date}T00:00:00Z`);
}

/** xScale maps a time to x, from t0 at left to t1 at x1; a single day sits at left. */
export function xScale(t0: number, t1: number, x1: number): (t: number) => number {
  const span = t1 - t0;
  return (t) => (span > 0 ? left + ((t - t0) / span) * (x1 - left) : left);
}

/** yScale maps a value of r to y, min at the bottom; a zero-width range puts its value in the middle. */
export function yScale(r: Range): (v: number) => number {
  const span = r.max - r.min;
  if (!(span > 0)) {
    return () => (top + bottom) / 2;
  }
  return (v) => bottom - ((v - r.min) / span) * (bottom - top);
}

const f1 = (v: number) => v.toFixed(1);

/**
 * linePath returns the SVG path of values at xs; a null value breaks the
 * line. With step, a change of value draws a vertical step on the day it
 * changes, as TTM EPS does on a filing.
 */
export function linePath(
  xs: readonly number[],
  values: readonly (number | null)[],
  y: (v: number) => number,
  step: boolean,
): string {
  let d = "";
  let prev: number | null = null;
  values.forEach((v, i) => {
    const x = xs[i];
    if (v === null || x === undefined) {
      prev = null;
      return;
    }
    if (prev === null) {
      d += `M${f1(x)},${f1(y(v))}`;
    } else {
      if (step && prev !== v) {
        d += `L${f1(x)},${f1(y(prev))}`;
      }
      d += `L${f1(x)},${f1(y(v))}`;
    }
    prev = v;
  });
  return d;
}

/**
 * bandSegments returns the line of a P/E band as runs of points: the day's
 * TTM EPS times multiple, with a step on each day the TTM EPS changes. A day
 * without a positive TTM EPS ends a run.
 */
export function bandSegments(
  xs: readonly number[],
  ttmEps: readonly (number | null)[],
  multiple: number,
  y: (v: number) => number,
): [number, number][][] {
  const runs: [number, number][][] = [];
  let run: [number, number][] = [];
  let prev: number | null = null;
  ttmEps.forEach((e, i) => {
    const x = xs[i];
    const p = bandPrice(e, multiple).value;
    if (p === null || x === undefined) {
      if (run.length > 0) {
        runs.push(run);
      }
      run = [];
      prev = null;
      return;
    }
    if (prev !== null && prev !== p) {
      run.push([x, y(prev)]);
    }
    run.push([x, y(p)]);
    prev = p;
  });
  if (run.length > 0) {
    runs.push(run);
  }
  return runs;
}

/** pointsPath joins runs of points into one SVG path. */
export function pointsPath(runs: readonly (readonly [number, number][])[]): string {
  return runs
    .map((r) => r.map(([x, y], i) => `${i === 0 ? "M" : "L"}${f1(x)},${f1(y)}`).join(""))
    .join("");
}

/** shadePath returns the area between two bands whose runs cover the same days. */
export function shadePath(
  upper: readonly (readonly [number, number][])[],
  lower: readonly (readonly [number, number][])[],
): string {
  return upper
    .map((u, i) => {
      const l = lower[i] ?? [];
      const pts = [...u, ...[...l].reverse()];
      return pts.map(([x, y], j) => `${j === 0 ? "M" : "L"}${f1(x)},${f1(y)}`).join("") + "Z";
    })
    .join("");
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** Tick is a month on the time axis. */
export interface Tick {
  t: number;
  label: string;
}

/**
 * monthTicks returns the month starts after t0 up to t1: each month in a 6M
 * range, every second month in a 1Y range, and every sixth in a 3Y range.
 */
export function monthTicks(t0: number, t1: number, range: ChartRange): Tick[] {
  const every = range === "6M" ? 1 : range === "1Y" ? 2 : 6;
  const start = new Date(t0);
  const out: Tick[] = [];
  for (
    let d = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + 1, 1));
    d.getTime() <= t1;
    d = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1))
  ) {
    if (d.getUTCMonth() % every !== 0) {
      continue;
    }
    out.push({
      t: d.getTime(),
      label: `${months[d.getUTCMonth()] ?? ""} '${String(d.getUTCFullYear()).slice(2)}`,
    });
  }
  return out;
}

/** dayLabel shows a YYYY-MM-DD date as "30 Sep 2026". */
export function dayLabel(date: string): string {
  const d = new Date(dayTime(date));
  return `${String(d.getUTCDate())} ${months[d.getUTCMonth()] ?? ""} ${String(d.getUTCFullYear())}`;
}

/**
 * labelSlots places the band labels at the right edge: each at the y of its
 * band's last point, moved up to keep 15 units from the label below it.
 * Labels outside the plot, or pushed out of it, are left out.
 */
export function labelSlots(ends: readonly { multiple: number; y: number }[]): {
  multiple: number;
  y: number;
}[] {
  const inside = ends.filter((e) => e.y >= top + 8 && e.y <= bottom - 2);
  inside.sort((a, b) => b.y - a.y);
  const out: { multiple: number; y: number }[] = [];
  let last = Number.POSITIVE_INFINITY;
  for (const e of inside) {
    const y = Math.min(e.y, last - 15);
    last = y;
    if (y < top + 8) {
      break;
    }
    out.push({ multiple: e.multiple, y });
  }
  return out;
}

/** nearestIndex returns the index of the x in xs (ascending) nearest to vx. */
export function nearestIndex(xs: readonly number[], vx: number): number {
  let lo = 0;
  let hi = xs.length - 1;
  if (hi <= 0) {
    return 0;
  }
  while (hi - lo > 1) {
    const m = (lo + hi) >> 1;
    if ((xs[m] ?? 0) < vx) {
      lo = m;
    } else {
      hi = m;
    }
  }
  return Math.abs((xs[lo] ?? 0) - vx) <= Math.abs((xs[hi] ?? 0) - vx) ? lo : hi;
}

/** multipleLabel shows a band's multiple: "25×", "17.5×". */
export function multipleLabel(m: number): string {
  return `${String(m)}×`;
}

/** priceTick shows a value of the price axis: "$50", "$1,250", "$2.5". */
export function priceTick(v: number): string {
  const r = Number(v.toFixed(6));
  return `$${r >= 1000 ? r.toLocaleString("en-US") : String(r)}`;
}
