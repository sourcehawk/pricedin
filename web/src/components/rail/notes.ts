import type { Evaluation, Levers } from "@/engine";
import { pct, pp, times, usd } from "@/components/format";

// The notes under the levers, from the prototype's copy. They read the
// engine's rule codes and numbers; they compute nothing themselves.

const half = (v: number): number => Math.round(v * 2) / 2;

/** typicalText describes the typical P/E: "typical 22.5× (the median of the last 36 months)". */
export function typicalText(ev: Evaluation): string {
  const t = ev.exitPe.typical;
  const pe = t.pe.value === null ? "n/a" : times(half(t.pe.value));
  return `typical ${pe} (the median of the last ${String(t.months)} months)`;
}

/** exitPeNote explains the exit P/E in use, by its mode and the rule that set it. */
export function exitPeNote(ev: Evaluation): string {
  const x = ev.exitPe;
  const now = ev.peNow.value;
  const inUse = x.inUse.value;
  if (now === null || inUse === null) {
    return "A P/E needs positive EPS. Loss-making companies need a different method.";
  }
  const today = times(now);
  switch (inUse.rule) {
    case "fade":
      return `Today the market pays ${today} earnings. Investors pay more for fast growth, and growth usually slows. If growth slows from ${pct(x.g0.value)} now to about ${pct(x.gExit.value)} when you sell, the estimate is ${times(inUse.pe)}.`;
    case "slowGrowerTypical": {
      const g0 = x.g0.value;
      const growth =
        g0 !== null && g0 < 0 ? `shrinking (${pct(g0)})` : `growing slowly (${pct(g0)})`;
      return `Today the market pays ${today} earnings, above the stock's ${typicalText(ev)}. With earnings ${growth}, a P/E above the usual level rarely lasts, so the estimate uses that typical ${times(inUse.pe)}.`;
    }
    case "slowGrowerKeep":
      return x.g0.value === null
        ? `Today the market pays ${today} earnings. The history is too short for a growth trend, so the estimate keeps today's P/E.`
        : `Today the market pays ${today} earnings, close to the stock's ${typicalText(ev)}. Growth is already slow (${pct(x.g0.value)}), so the estimate keeps today's P/E.`;
    case "lowPeKeep":
      return `Today the market pays only ${today} earnings, so the estimate keeps it rather than going lower.`;
    case "today":
      return `Assumes the market still pays today's ${today} earnings when you sell.`;
    case "trailing":
      return `Uses the stock's typical P/E: the median of its quarter-end P/Es over the last ${String(x.typical.months)} months, ${times(inUse.pe)}. A P/E often drifts back toward its usual level.`;
    case "custom": {
      const est = x.estimate.value;
      return `Your own number. For comparison, the estimate is ${times(est?.pe)} and today's P/E is ${today}.`;
    }
  }
}

/** baselineNote explains the growth baseline in use. */
export function baselineNote(ev: Evaluation, levers: Levers): string {
  switch (ev.baselines.active) {
    case "weighted":
      return "Year-over-year EPS growth of the last 8 quarters, with recent quarters counting most: a quarter's weight halves every two quarters back. Follows recent execution without leaning on one quarter.";
    case "today":
      return "EPS growth of the latest quarter against the same quarter a year earlier. The freshest reading, but one unusual quarter moves it.";
    case "trailing": {
      const t = ev.baselines.trailing;
      const requested = levers.baseline.trailingMonths ?? 12;
      const rounded =
        t.months !== requested
          ? ` Rounded to ${String(t.months)} months, because results come out every 3 months.`
          : "";
      return `Average yearly growth of TTM EPS over the last ${String(t.months)} months: from ${usd(t.fromTtm.value)} to ${usd(t.toTtm.value)}.${rounded} A short period reacts fast, a long one smooths more.`;
    }
    case "custom":
      return levers.baseline.custom?.yearly === true
        ? "Your own growth rate for each year of the holding period. The page turns them into one compound average."
        : "Your own assumption for yearly EPS growth.";
  }
}

/** TrajectoryParts is the trajectory note in parts, so the rates can stand out. */
export interface TrajectoryParts {
  lead: string;
  rates: string;
  tail: string;
}

/** trajectoryParts describes the trajectory fill; null without a weighted trend. */
export function trajectoryParts(ev: Evaluation): TrajectoryParts | null {
  const t = ev.trajectory.value;
  if (t === null) {
    return null;
  }
  return {
    lead: `Over the last year the weighted trend went from ${pct(t.previous.value)} to ${pct(ev.baselines.weighted.value)} (${pp(t.slope)}). If it keeps ${t.slope < 0 ? "slowing" : "speeding up"}, but less each year: `,
    rates: t.rates.map((r) => pct(r)).join(", "),
    tail: `.${t.capped ? ` Growth is not projected above its highest reading of the last two years (${pct(t.highest)}).` : ""}`,
  };
}

/** trajectoryText is trajectoryParts as one sentence; empty without a weighted trend. */
export function trajectoryText(ev: Evaluation): string {
  const p = trajectoryParts(ev);
  return p === null ? "" : p.lead + p.rates + p.tail;
}
