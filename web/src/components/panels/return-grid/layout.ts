import type { Grid, GridTag, Tone } from "@/engine";

/** GridView is what the cells show: the yearly return, the total return or the exit share price. */
export type GridView = "cagr" | "cum" | "px";

/** TagStyle is how a row tag looks and what it stands for. */
export interface TagStyle {
  /** The text of the tag. */
  label: string;
  /** The class next to "tag"; empty for the custom tag, which the panel styles itself. */
  className: string;
  /** The outline color of the tag's row. */
  color: string;
  /** What the tag stands for, in the legend. */
  legend: string;
}

export const tagStyles: Record<GridTag, TagStyle> = {
  required: { label: "Req", className: "req", color: "var(--accent)", legend: "required growth" },
  weighted: { label: "Wtd", className: "wtd", color: "var(--s3)", legend: "weighted trend" },
  today: { label: "Today", className: "rec", color: "var(--ink)", legend: "latest quarter" },
  trailing: { label: "Trl", className: "lt", color: "var(--muted)", legend: "trailing growth" },
  custom: { label: "Custom", className: "", color: "var(--s4)", legend: "your custom rate" },
};

/** The order of the tags in a row header and in the legend. */
export const tagOrder: readonly GridTag[] = ["required", "weighted", "today", "trailing", "custom"];

/** toneClass is the cell class of a tone; a neutral tone does not occur in the grid. */
export function toneClass(t: Tone): "good" | "warn" | "bad" {
  return t === "neutral" ? "warn" : t;
}

/**
 * exampleRow is the row the walkthrough uses as its example: the untagged
 * row nearest the middle of the grid that is not the YOU row; -1 when every
 * row is tagged.
 */
export function exampleRow(grid: Grid): number {
  const you = grid.you.value?.row ?? -1;
  const mid = (grid.rows.length - 1) / 2;
  let best = -1;
  grid.rows.forEach((r, i) => {
    if (r.tags.length > 0 || i === you) {
      return;
    }
    if (best < 0 || Math.abs(i - mid) < Math.abs(best - mid)) {
      best = i;
    }
  });
  return best;
}

/** taggedRow is the index of the row that carries tag; -1 without it. */
export function taggedRow(grid: Grid, tag: GridTag): number {
  return grid.rows.findIndex((r) => r.tags.includes(tag));
}

/** growthLabel is a row's growth as its header shows it: the exact baseline row always with one decimal. */
export function growthLabel(grid: Grid, i: number): string {
  const r = grid.rows[i];
  if (r === undefined) {
    return "n/a";
  }
  const decimals = r.exact || grid.step < 0.05 ? 1 : 0;
  return `${(r.growth * 100).toFixed(decimals)}%`;
}

/** peLabel is an exit P/E without a trailing ".0": 20, 22.5. */
export function peLabel(pe: number): string {
  return pe % 1 === 0 ? String(pe) : pe.toFixed(1);
}
