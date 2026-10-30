import type { ReactNode } from "react";
import { pct, points, signedPct } from "@/components/format";
import { LineSample } from "@/components/Swatch";
import type { StepContext } from "@/components/walkthrough/steps";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Evaluation } from "@/engine";

/** peakTie is the engine's tolerance for equal required growths. */
const peakTie = 1e-9;

/** demandingSteps are the walkthrough steps of the how-demanding chart. */
export function demandingSteps({ ticker }: StepContext): WalkthroughStep[] {
  return [
    {
      title: (
        <>
          <LineSample color="var(--accent)" /> The required growth line
        </>
      ),
      body: (
        <p>
          Each point is the yearly EPS growth you would have needed if you had bought on that date,
          using today&apos;s levers: your target return, holding period and exit P/E.
        </p>
      ),
      highlight: ["req"],
      levers: ["targetReturn", "holdingYears", "exitPe"],
      you: (e) => {
        const p = e.howDemanding.points;
        return (
          <>
            Bought in {p[0]?.label}, you would have needed <b>{pct(p[0]?.required.value)}</b> a
            year. Today you need <b>{pct(p.at(-1)?.required.value)}</b>.
          </>
        );
      },
    },
    {
      title: (
        <>
          <LineSample color="var(--ink)" /> The delivered growth line
        </>
      ),
      body: (
        <p>
          Each point is how much TTM EPS (earnings per share over the last twelve months) actually
          grew over the year to that quarter: the record the business was building when you would
          have bought.
        </p>
      ),
      highlight: ["del"],
      you: (e) => {
        const p = e.howDemanding.points;
        return (
          <>
            In the year before today, TTM EPS grew <b>{signedPct(p.at(-1)?.delivered.value)}</b>. A
            year earlier it had grown <b>{signedPct(p.at(-5)?.delivered.value)}</b>.
          </>
        );
      },
    },
    {
      title: "Follow the required line over time",
      body: (
        <p>
          When it falls, the stock got easier to own, because earnings grew faster than the price.
          When it rises, the price ran ahead of earnings.
        </p>
      ),
      highlight: ["req"],
      you: (e) => {
        const p = e.howDemanding.points;
        const a = p[0]?.required.value ?? null;
        const b = p.at(-1)?.required.value ?? null;
        if (a === null || b === null) {
          return null;
        }
        const size = points(b - a);
        return b > a ? (
          <>
            The required growth rose by <b>{size} points</b>, so the stock got more demanding: the
            price ran ahead of earnings.
          </>
        ) : (
          <>
            The required growth fell by <b>{size} points</b>, so the stock got easier to own.
          </>
        );
      },
    },
    {
      title: "Compare the two lines",
      body: (
        <p>
          When the required line sits above the delivered line, the price asked for more than the
          business had just delivered. When it sits below, the business was growing faster than the
          price required.
        </p>
      ),
      highlight: ["req", "del"],
      you: (e) => (
        <>
          In{" "}
          <b>
            {e.howDemanding.requiredAboveDelivered} of {e.howDemanding.compared}
          </b>{" "}
          quarters the price asked for more growth than the business had just delivered.
        </>
      ),
    },
    {
      title: "Compare today with the past",
      body: (
        <p>
          If today&apos;s required point is the highest on the chart, the stock is more demanding
          now than at any time in the range.
        </p>
      ),
      highlight: ["last", "peak"],
      you: (e) => todayLine(e),
      takeaway: (e) => {
        const last = e.howDemanding.points.at(-1)?.required.value ?? null;
        const med = e.howDemanding.typicalRequired.value;
        if (last === null || med === null) {
          return null;
        }
        const tail =
          last > med + 0.01
            ? `more than the typical ${pct(med)} over this period: the stock is more demanding to own than usual.`
            : last < med - 0.01
              ? `less than the typical ${pct(med)} over this period: the stock is easier to own than usual.`
              : `close to the typical ${pct(med)} over this period.`;
        return (
          <>
            <strong>{ticker} in one line:</strong> buying today needs {pct(last)} growth a year,{" "}
            {tail}
          </>
        );
      },
    },
  ];
}

function todayLine(e: Evaluation): ReactNode {
  const d = e.howDemanding;
  const last = d.points.at(-1)?.required.value ?? null;
  const peak = d.peak.value;
  if (last === null || peak === null) {
    return null;
  }
  // The engine keeps the first of tied peaks, so today can tie without being the peak.
  if (last >= peak.growth - peakTie) {
    return (
      <>
        Today&apos;s <b>{pct(last)}</b> is the most demanding point in the range.
      </>
    );
  }
  return (
    <>
      Today&apos;s <b>{pct(last)}</b> is below the peak of {pct(peak.growth)} in{" "}
      {d.points[peak.index]?.label}.
    </>
  );
}
