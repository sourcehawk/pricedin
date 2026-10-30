import { pct, signedPct } from "@/components/format";
import type { Evaluation, Tiles } from "@/engine";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";

// The financials walkthrough, from the prototype's financials guide.

/** tilesOf returns the tiles; a step's line is left out when they are missing. */
function tilesOf(ev: Evaluation): Tiles {
  const t = ev.financials.tiles.value;
  if (t === null) {
    throw new Error("no tiles");
  }
  return t;
}

function change(c: { value: number | null }, digits = 0): string {
  if (c.value === null) {
    throw new Error("no change");
  }
  return signedPct(c.value, digits);
}

/** verdictLine says whether revenue and operating income keep up with the required growth. */
function verdictLine(ev: Evaluation, t: Tiles, inOneLine: boolean): string {
  const r = ev.required.growth.value;
  const rv = t.revenue.change.value;
  const op = t.operatingIncome.change.value;
  if (r === null || rv === null || op === null) {
    return "";
  }
  if (rv >= r && op >= r) {
    return inOneLine
      ? "The business is growing as fast as the price requires."
      : "the business is growing fast enough at the moment.";
  }
  if (op >= r) {
    return inOneLine
      ? "Profit keeps up with the price only through wider margins."
      : "profit is growing fast enough, but mostly through wider margins, since sales grow much slower. Margins cannot widen forever.";
  }
  return inOneLine
    ? "The business grows slower than the price requires."
    : "the core business is growing slower than the price requires.";
}

export const intro =
  "Earnings per share is the end result. These numbers show where it came from and whether it is likely to last.";

export const watchOut =
  "Single quarters move with timing, such as large orders or tax payments. Look at several quarters before you draw a conclusion.";

/** financialsSteps are the steps of the financials walkthrough for the stock ticker. */
export function financialsSteps(ticker: string): WalkthroughStep[] {
  return [
    {
      title: "Start with revenue",
      body: (
        <p>
          Revenue (sales) is the hardest number to inflate. If EPS grows much faster than revenue
          for a long time, the difference must come from higher margins, share buybacks or one-off
          items.
        </p>
      ),
      highlight: ["tile-revenue"],
      you: (ev) => {
        const c = tilesOf(ev).revenue.change;
        return (
          <>
            Revenue {(c.value ?? 0) >= 0 ? "grew" : "changed"} <b>{change(c)}</b> against the same
            quarter a year earlier.
          </>
        );
      },
    },
    {
      title: "Check operating income",
      body: (
        <p>
          Operating income is the profit from the core business. The operating margin is that profit
          as a share of revenue. A rising margin means each dollar of sales brings more profit. That
          can last, but margins cannot rise forever.
        </p>
      ),
      highlight: ["tile-operatingIncome", "tile-margin"],
      you: (ev) => {
        const t = tilesOf(ev);
        const m = t.operatingMargin;
        return (
          <>
            Operating income grew <b>{change(t.operatingIncome.change)}</b> and the margin moved
            from {pct(m.previous.value)} to <b>{pct(m.latest.value)}</b>.
          </>
        );
      },
    },
    {
      title: "Compare operating costs",
      body: (
        <p>
          Operating costs are what the company spent to run the business. If they grow faster than
          revenue for several quarters, the margin shrinks and profit growth slows, even while sales
          grow.
        </p>
      ),
      highlight: ["tile-operatingCosts", "tile-revenue"],
      you: (ev) => {
        const t = tilesOf(ev);
        return (
          <>
            Costs moved <b>{change(t.operatingCosts.change)}</b> while revenue moved{" "}
            <b>{change(t.revenue.change)}</b>:{" "}
            {t.operatingCosts.tone.value === "warn"
              ? "costs outpaced sales, which squeezes the margin."
              : "sales kept pace with costs or outpaced them."}
          </>
        );
      },
    },
    {
      title: "Watch the share count",
      body: (
        <p>
          When a company issues new shares, for example to pay employees in stock or to buy another
          company, each existing share owns less of the profit. That is dilution. A rising share
          count makes EPS grow slower than profit, and a falling one (buybacks) makes it grow
          faster. The price chart marks quarters where the share count changed by 2% or more.
        </p>
      ),
      highlight: ["tile-dilutedShares"],
      you: (ev) => {
        const c = tilesOf(ev).dilutedShares.change;
        const v = c.value ?? 0;
        return (
          <>
            The share count changed <b>{change(c, 1)}</b> in a year:{" "}
            {v > 0.005
              ? "dilution held EPS growth below profit growth."
              : v < -0.005
                ? "buybacks lifted EPS growth above profit growth."
                : "too small to matter."}
          </>
        );
      },
    },
    {
      title: "Compare free cash flow",
      body: (
        <p>
          Free cash flow (FCF) is the cash the business brings in, minus what it spends on equipment
          and buildings. If it stays well below earnings for several quarters, the earnings are less
          reliable than they look.
        </p>
      ),
      highlight: ["tile-freeCashFlow", "tile-eps"],
      you: (ev) => {
        const t = tilesOf(ev);
        const fc = t.freeCashFlow.change.value ?? 0;
        const ep = t.eps.change.value ?? 0;
        return (
          <>
            Free cash flow moved <b>{change(t.freeCashFlow.change)}</b> while EPS moved{" "}
            <b>{change(t.eps.change)}</b>
            {fc < ep - 0.15 ? ": cash lagged earnings this quarter." : "."}
          </>
        );
      },
    },
    {
      title: "Compare each quarter with a year earlier",
      body: (
        <p>
          Many businesses are seasonal, so each YoY (year over year) figure compares a quarter with
          the same quarter one year earlier.
        </p>
      ),
      highlight: ["tile-change"],
      you: (ev) => {
        const t = tilesOf(ev);
        return (
          <>
            Each figure compares {t.label} with {t.compareLabel}, so seasonal swings cancel out.
          </>
        );
      },
    },
    {
      title: "Use Compare",
      body: (
        <p>
          Compare puts several metrics on one chart. &ldquo;Since&rdquo; the first quarter shows
          each metric&rsquo;s total change over the period, so you can see which grew fastest.
          &ldquo;Each quarter&rdquo; shows the step from one quarter to the next. For example, EPS
          rising above operating income means something other than the business, such as buybacks or
          lower taxes, lifted EPS. The table below the chart shows which metrics moved together from
          quarter to quarter.
        </p>
      ),
      highlight: ["compare-switch"],
      you: () => (
        <>
          Click <b>Compare</b> in the switch at the top of this panel to put several metrics on one
          chart.
        </>
      ),
    },
    {
      title: "Tie it back to the price",
      body: (
        <p>
          If revenue and operating income support the growth the price requires, the priced-in
          number is believable. If only EPS does, look deeper before you trust it.
        </p>
      ),
      highlight: ["tile-revenue", "tile-operatingIncome"],
      levers: ["targetReturn"],
      you: (ev) => {
        const t = tilesOf(ev);
        const r = ev.required.growth.value;
        if (r === null) {
          throw new Error("no required growth");
        }
        return (
          <>
            At your target return, the price needs <b>{pct(r)}</b> EPS growth a year. Revenue grows{" "}
            <b>{change(t.revenue.change)}</b> and operating income{" "}
            <b>{change(t.operatingIncome.change)}</b>: {verdictLine(ev, t, false)}
          </>
        );
      },
      takeaway: (ev) => {
        const t = tilesOf(ev);
        return (
          <>
            <strong>{ticker} in one line:</strong> revenue {signedPct(t.revenue.change.value, 0)},
            operating income {signedPct(t.operatingIncome.change.value, 0)}, free cash flow{" "}
            {signedPct(t.freeCashFlow.change.value, 0)} against a year earlier.{" "}
            {verdictLine(ev, t, true)}
          </>
        );
      },
    },
  ];
}
