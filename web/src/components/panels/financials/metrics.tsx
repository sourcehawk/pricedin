import type { ReactNode } from "react";
import { Swatch } from "@/components/Swatch";
import { money, shareCount, usd } from "@/components/format";
import type { Metric, Missing, Tone } from "@/engine";

/** cls joins class names, leaving out the missing ones. */
export function cls(...names: (string | undefined | false)[]): string {
  return names.filter((n) => typeof n === "string" && n !== "").join(" ");
}

/** MetricInfo names a metric and explains it, as the prototype does. */
export interface MetricInfo {
  name: string;
  /** The name inside a sentence. */
  lower: string;
  /** The label of its button in the metric switch. */
  short: string;
  /** The explanation in its tooltip. */
  tip: ReactNode;
  /** Its line color in the compare view, a token. */
  color: string;
}

export const metricInfo: Record<Metric, MetricInfo> = {
  revenue: {
    name: "Revenue",
    lower: "revenue",
    short: "Revenue",
    tip: "Total sales in the quarter. Revenue growth is the hardest number for a company to inflate, so it is the best check on EPS growth.",
    color: "var(--accent)",
  },
  operatingCosts: {
    name: "Operating costs",
    lower: "operating costs",
    short: "Op. costs",
    tip: (
      <>
        Everything the company spent to run the business: the cost of the goods it sold plus
        salaries, research, marketing and administration. It equals revenue minus operating income.
        Rising costs are not bad by themselves, because a growing business spends more, and cutting
        research or marketing can hurt later. What matters is costs against revenue: the change
        shows as <Swatch color="var(--warn)" /> when costs grow faster than revenue, because that
        squeezes the margin, and as <Swatch color="var(--muted)" /> otherwise.
      </>
    ),
    color: "var(--s6)",
  },
  operatingIncome: {
    name: "Operating income",
    lower: "operating income",
    short: "Op. income",
    tip: "Profit from the core business, before interest and taxes. It equals revenue minus operating costs.",
    color: "var(--s3)",
  },
  eps: {
    name: "Diluted EPS",
    lower: "diluted EPS",
    short: "EPS",
    tip: "Profit per share for the quarter, counting shares that options and grants will create.",
    color: "var(--warn)",
  },
  freeCashFlow: {
    name: "Free cash flow",
    lower: "free cash flow",
    short: "FCF",
    tip: "Cash from running the business minus spending on equipment and buildings. It is the cash the company can return to shareholders or use to grow. If it stays below earnings for long, the earnings are less reliable than they look.",
    color: "var(--s4)",
  },
  dilutedShares: {
    name: "Diluted shares",
    lower: "diluted shares",
    short: "Shares",
    tip: (
      <>
        The number of shares, counting stock options and grants that will turn into shares. When it
        rises (dilution), each share owns a smaller slice of the profit, so EPS grows slower than
        profit. When it falls (buybacks), EPS grows faster than profit. An increase shows as{" "}
        <Swatch color="var(--bad)" />.
      </>
    ),
    color: "var(--ink)",
  },
};

export const marginTip =
  "Operating income divided by revenue: how many cents of profit each dollar of sales brings. The change is in percentage points (pp).";

/** toneClass is the text class of a tone: good is green, bad red, warn amber, neutral muted. */
export function toneClass(t: Tone | null): string {
  switch (t) {
    case "good":
      return "up";
    case "bad":
      return "down";
    case "warn":
      return "warnc";
    default:
      return "muted";
  }
}

/** missingText says in a few words why a change is missing. */
export function missingText(m: Missing): string {
  switch (m.reason) {
    case "non_positive_base":
      return "the year-earlier value was not positive";
    case "insufficient_history":
      return "not enough quarters";
    default:
      return "not reported";
  }
}

/** metricValue shows a value of metric m; decimals applies to amounts in billions. */
export function metricValue(m: Metric, x: number | null | undefined, decimals = 1): string {
  switch (m) {
    case "eps":
      return usd(x, 2);
    case "dilutedShares":
      return shareCount(x);
    default:
      return money(x, decimals);
  }
}
