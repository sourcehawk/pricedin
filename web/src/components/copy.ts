import type { ReactNode } from "react";
import type { Evaluation } from "@/engine";

// Copy helpers that the panels, the rail and their walkthroughs share, so
// the same fact reads the same way everywhere.

/** baselineName names the baseline in use, as the verdict and the notes say it; the custom one carries its own "your". */
export function baselineName(ev: Evaluation): string {
  switch (ev.baselines.active) {
    case "weighted":
      return "weighted trend";
    case "today":
      return "latest quarter";
    case "trailing":
      return `trailing ${String(ev.baselines.trailing.months)}-month growth`;
    case "custom":
      return "your custom growth";
  }
}

/** baselineLabel names the baseline in use without a leading "your", for a sentence that puts "your" or "the" before it. */
export function baselineLabel(ev: Evaluation): string {
  return baselineName(ev).replace(/^your /, "");
}

/** dividendsReach reports whether the dividends alone reach the target, so no EPS growth is required. */
export function dividendsReach(ev: Evaluation): boolean {
  return (
    ev.required.growth.value === null && ev.required.growth.reason === "dividends_reach_target"
  );
}

/** orDividends returns a step line that shows noGrowth in place of line when the dividends alone reach the target. */
export function orDividends(
  noGrowth: ReactNode,
  line: (ev: Evaluation) => ReactNode,
): (ev: Evaluation) => ReactNode {
  return (ev) => (dividendsReach(ev) ? noGrowth : line(ev));
}

/** joinNames lists names as a sentence does: "Bear", "Bear and Bull", "Bear, Base and Bull". */
export function joinNames(names: readonly string[]): string {
  if (names.length <= 1) {
    return names.join("");
  }
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}`;
}
