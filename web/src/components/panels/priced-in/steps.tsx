import type { CSSProperties } from "react";
import type { Evaluation } from "@/engine";
import { pct, pp, signedPct, times, usd, years } from "@/components/format";
import { baselineName } from "@/components/rail/notes";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";

const swatch = (color: string) => (
  <i className="sq" style={{ "--c": color } as CSSProperties} aria-hidden="true" />
);

const pill = (tone: "good" | "warn" | "bad", text: string) => (
  <span className={`pill ${tone}`}>{text}</span>
);

function compareLine(ev: Evaluation) {
  const b = ev.baselines;
  const R = ev.required.growth.value;
  const values = [b.weighted.value, b.today.value, b.trailing.growth.value];
  const below = R === null ? 0 : values.filter((v) => v !== null && v < R).length;
  const count =
    below === 3
      ? "All three are below"
      : below > 0
        ? `${String(below)} of 3 are below`
        : "None is below";
  return (
    <>
      Weighted <b>{pct(b.weighted.value)}</b>, today <b>{pct(b.today.value)}</b>, trailing{" "}
      <b>{pct(b.trailing.growth.value)}</b>. {count} the <b>{pct(R)}</b> the price requires.
    </>
  );
}

function verdictLine(ev: Evaluation, sym: string) {
  const v = ev.verdict.value;
  const R = ev.required.growth.value;
  const gap = ev.gap.value;
  if (v === null || R === null || gap === null) {
    return null;
  }
  const word = { undemanding: "Undemanding", tight: "Tight", demanding: "Demanding" }[v];
  const tone = ({ undemanding: "good", tight: "warn", demanding: "bad" } as const)[v];
  let next;
  if (v === "demanding") {
    next = (
      <>
        It would become {pill("warn", "Tight")} at about <b>{pct(ev.required.tightFrom.value)}</b>{" "}
        growth.
      </>
    );
  } else if (v === "tight") {
    next = (
      <>
        It would become {pill("good", "Undemanding")} at about{" "}
        <b>{pct(ev.required.undemandingFrom.value)}</b> growth.
      </>
    );
  } else {
    next = (
      <>It stays {pill("good", "Undemanding")} as long as growth holds above the requirement.</>
    );
  }
  return (
    <>
      {sym} shows {pill(tone, word)}: your baseline ({baselineName(ev)},{" "}
      {pct(ev.baselines.growth.value)}) is <b>{pp(gap)}</b> from the {pct(R)} required. {next}
    </>
  );
}

function directionLine(ev: Evaluation) {
  const recent = ev.quarters.slice(-4).map((q) => signedPct(q.yoy.value, 0));
  const d = ev.baselines.yoyShift.value;
  const trend =
    d === null
      ? ""
      : d < -0.03
        ? "Growth is slowing, so expect the weighted trend to keep drifting down."
        : d > 0.03
          ? "Growth is speeding up, so the weighted trend may still understate it."
          : "Growth is steady.";
  return (
    <>
      The last four quarters grew <b>{recent.join(", ")}</b>. {trend}
    </>
  );
}

/** StepContext holds the lever values that the steps quote. */
export interface StepContext {
  sym: string;
  /** The target return. */
  r: number;
  /** The holding period in years. */
  n: number;
  price: number;
  eps: number;
}

/** pricedInSteps are the walkthrough steps of the priced-in panel. */
export function pricedInSteps({ sym, r, n, price, eps }: StepContext): WalkthroughStep[] {
  return [
    {
      title: "The big number is the bar to clear",
      highlight: ["required"],
      body: (
        <p>
          It is the average yearly growth in earnings per share (EPS) that the company needs, so
          that you earn your target return. It assumes you sell at the exit P/E: the
          price-to-earnings ratio you expect the market to pay when you sell.
        </p>
      ),
      you: (ev) => (
        <>
          {sym} must grow EPS <b>{pct(ev.required.growth.value)}</b> a year on average for{" "}
          {years(n)} to give you {pct(r)} a year.
        </>
      ),
    },
    {
      title: "What it takes in dollars",
      highlight: ["exit"],
      body: (
        <p>
          Under the big number, the same requirement in dollars: the share price you need when you
          sell, and the EPS the company must reach so that your exit P/E gives that price.
        </p>
      ),
      you: (ev) => (
        <>
          In dollars: EPS must go from <b>{usd(eps)}</b> today to{" "}
          <b>{usd(ev.required.eps.value)}</b> in {years(n)}. At {times(ev.exitPe.inUse.value?.pe)}{" "}
          that is a share price of <b>{usd(ev.required.exitPrice.value)}</b>, which turns
          today&apos;s {usd(price)} into {pct(r)} a year.
        </>
      ),
    },
    {
      title: "Compare it with what the company has done",
      highlight: ["baselines", "ruler-marks"],
      levers: ["baseline", "trailingMonths"],
      body: (
        <p>
          There are three reference points, the same as the growth baseline options in the left
          panel. The weighted trend averages the last 8 quarters of year-over-year growth, with the
          latest quarters counting most. Today uses only the latest quarter. Trailing growth is the
          average yearly growth over the months you set next to Trailing growth in the left panel
          (12 by default). On the scale, the large dot is the requirement. Any growth rate in the
          shaded part of the scale {swatch("var(--good)")}beats it. A smaller dot to the left of the
          large dot falls short.
        </p>
      ),
      you: compareLine,
    },
    {
      title: "Read the gap",
      highlight: ["gap"],
      body: (
        <p>
          The gap is your chosen baseline minus the requirement, in percentage points (pp). A small
          negative gap means the company must speed up a little. A large negative gap means the
          price already assumes a much better future than the company&apos;s record shows.
        </p>
      ),
      you: (ev) => {
        const gap = ev.gap.value;
        const note =
          gap === null
            ? ""
            : ev.verdict.value === "undemanding"
              ? "The company already grows fast enough."
              : gap > -0.03
                ? "It needs only a small step up."
                : "It needs a clear acceleration, which the record does not show yet.";
        return (
          <>
            Your baseline ({baselineName(ev)}, {pct(ev.baselines.growth.value)}) is <b>{pp(gap)}</b>{" "}
            from the requirement. {note}
          </>
        );
      },
    },
    {
      title: "Read the verdict label",
      highlight: ["verdict"],
      levers: ["baseline"],
      body: (
        <div>
          The label under the big number sums up the gap in one word.
          <span className="vl">
            {pill("good", "Undemanding")} your growth baseline beats the requirement by more than
            1.5 points a year, so the company already grows fast enough.
          </span>
          <span className="vl">
            {pill("warn", "Tight")} the baseline is within 1.5 points either way, so you reach your
            target only if growth holds.
          </span>
          <span className="vl">
            {pill("bad", "Demanding")} the baseline falls short by more than 1.5 points, so the
            price assumes growth will speed up.
          </span>
          The label follows the baseline you pick in the left panel.
        </div>
      ),
      you: (ev) => verdictLine(ev, sym),
    },
    {
      title: "Check the direction with the quarterly chips",
      highlight: ["yoy"],
      body: (
        <p>
          Each chip compares one quarter&apos;s EPS with the same quarter a year earlier. If the
          numbers fall quarter after quarter, growth is slowing, and the weighted trend will keep
          dropping. If they rise, growth is speeding up.
        </p>
      ),
      you: directionLine,
    },
    {
      title: "Move the levers to test how fragile the result is",
      highlight: ["required"],
      levers: ["targetReturn", "exitPe"],
      body: (
        <p>
          Try the two levers marked in the left panel and watch the big number. A higher target
          return or a lower exit P/E raises the requirement. A longer holding period makes the exit
          P/E matter less each year. If a small lever change changes the verdict label, the
          investment has little room for error.
        </p>
      ),
      you: (ev) => {
        const s = ev.sensitivity.value;
        if (s === null) {
          return null;
        }
        return (
          <>
            Try it: a {times(s.lowerExitPe.exitPe)} exit would require{" "}
            <b>{pct(s.lowerExitPe.requiredGrowth.value)}</b>, and a{" "}
            {pct(s.lowerTarget.targetReturn)} target would require{" "}
            <b>{pct(s.lowerTarget.requiredGrowth.value)}</b>, against{" "}
            {pct(ev.required.growth.value)} now.
          </>
        );
      },
      takeaway: (ev) => {
        const gap = ev.gap.value;
        return (
          <>
            <strong>{sym} in one line:</strong> the price requires {pct(ev.required.growth.value)}{" "}
            EPS growth a year; the company&apos;s weighted trend is{" "}
            {pct(ev.baselines.weighted.value)}.{" "}
            {gap !== null && gap >= 0
              ? "The current pace clears the bar."
              : "The price assumes faster growth than the company shows now."}
          </>
        );
      },
    },
  ];
}
