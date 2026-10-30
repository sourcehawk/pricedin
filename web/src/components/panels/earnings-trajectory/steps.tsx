import type { ReactNode } from "react";
import { pct, signedPct, times, usd, years } from "@/components/format";
import { baselineLabel } from "@/components/copy";
import { LineSample, Swatch } from "@/components/Swatch";
import type { StepContext } from "@/components/walkthrough/steps";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Evaluation, Paths } from "@/engine";

/** pathsOf returns the trajectory; a walkthrough line that needs it is left out without one. */
function pathsOf(ev: Evaluation): Paths {
  if (ev.earningsPaths.value === null) {
    throw new Error("no trajectory");
  }
  return ev.earningsPaths.value;
}

/** trajectorySteps are the walkthrough steps of the earnings trajectory. */
export function trajectorySteps({ ticker, levers }: StepContext): WalkthroughStep[] {
  const n = levers.holdingYears;
  return [
    {
      title: (
        <>
          <LineSample color="var(--ink)" /> The reported EPS line
        </>
      ),
      body: (
        <p>
          The solid line is TTM EPS as the company reported it: earnings per share over each past 12
          months (TTM, trailing twelve months), one point per quarter. It ends at today.
        </p>
      ),
      highlight: ["hist"],
      you: (e) => {
        const p = pathsOf(e);
        const first = p.history[0];
        return (
          <>
            TTM EPS went from <b>{usd(first?.ttmEps.value)}</b> in {first?.label} to{" "}
            <b>{usd(p.future[0]?.baseline.value ?? levers.ttmEps)}</b> today.
          </>
        );
      },
    },
    {
      title: (
        <>
          <LineSample color="var(--muted)" dash /> The baseline line
        </>
      ),
      body: (
        <p>
          This dashed line continues from today at your growth baseline from the left panel. It
          shows where EPS ends up if the company keeps growing the way you assume.
        </p>
      ),
      highlight: ["base"],
      levers: ["baseline"],
      you: (e) => (
        <>
          At your {baselineLabel(e)} of <b>{pct(e.baselines.growth.value)}</b>, EPS reaches{" "}
          <b>{usd(pathsOf(e).future.at(-1)?.baseline.value)}</b> in {years(n)}.
        </>
      ),
    },
    {
      title: (
        <>
          <LineSample color="var(--accent)" dash /> The required line
        </>
      ),
      body: (
        <p>
          This dashed line is the EPS path that today&apos;s price needs for your target return at
          your exit P/E. Its end point is the EPS the company must reach by the time you sell.
        </p>
      ),
      highlight: ["req"],
      levers: ["targetReturn", "exitPe"],
      you: (e) => (
        <>
          The price needs EPS to reach <b>{usd(e.required.eps.value)}</b> in {years(n)}:{" "}
          <b>{pct(e.required.growth.value)}</b> growth a year.
        </>
      ),
    },
    {
      title: "Compare the slopes",
      body: (
        <p>
          Look at how steep the reported line was over the last year, then at the required path. If
          the required path is much steeper, the price assumes faster growth than the company has
          shown recently.
        </p>
      ),
      highlight: ["hist", "req"],
      you: (e) => {
        const last = pathsOf(e).history.at(-1)?.ttmYoy.value ?? null;
        const r = e.required.growth.value;
        if (last === null || r === null) {
          return null;
        }
        return (
          <>
            Over the last year TTM EPS grew <b>{signedPct(last)}</b>. The required path climbs{" "}
            <b>{pct(r)}</b> a year,{" "}
            {r > last ? "steeper than the recent record" : "no steeper than the recent record"}.
          </>
        );
      },
    },
    {
      title: "Read the shading",
      body: (
        <p>
          The shaded area is the gap between the two dashed lines.
          <span className="vl">
            <Swatch color="var(--bad)" />
            The company grows too slowly for your target, so you would earn less than you want.
          </span>
          <span className="vl">
            <Swatch color="var(--good)" />
            The company grows fast enough, so you would earn more than you want.
          </span>
          The wider the gap at the right edge, the bigger the difference.
        </p>
      ),
      highlight: ["shade", "req", "base"],
      you: (e) => shadingLine(e, ticker, levers.targetReturn),
    },
    {
      title: "Question the color",
      body: (
        <p>
          The color is only as good as your two assumptions, marked in the left panel. A big
          favorable area can come from an exit P/E set too high or a growth baseline that is too
          optimistic. It can also mean the market sees a risk you do not, such as a slowdown or
          earnings at a peak. A big unfavorable area can come from an exit P/E set too low or a
          baseline dragged down by one weak quarter. It can also mean the market expects an
          acceleration, or earnings that are only temporarily low. Either way, ask why the market
          disagrees with you.
        </p>
      ),
      highlight: ["shade"],
      levers: ["exitPe", "baseline"],
      you: (e) => colorLine(e),
    },
    {
      title: "Check for bends in the history",
      body: (
        <p>
          A reported line that flattens near today signals slowing growth, even if the longer
          trailing growth still looks strong.
        </p>
      ),
      highlight: ["hist"],
      you: (e) => {
        const h = pathsOf(e).history;
        const now = h.at(-1)?.ttmYoy.value ?? null;
        const before = h.at(-4)?.ttmYoy.value ?? null;
        if (now === null || before === null) {
          return null;
        }
        const shape =
          now < before - 0.02
            ? "the line is flattening."
            : now > before + 0.02
              ? "the line is getting steeper."
              : "the slope is steady.";
        return (
          <>
            TTM EPS growth went from <b>{signedPct(before, 0)}</b> three quarters ago to{" "}
            <b>{signedPct(now, 0)}</b> now: {shape}
          </>
        );
      },
      takeaway: (e) => {
        const gap = pathsOf(e).future.at(-1)?.gap.value ?? null;
        if (gap === null) {
          return null;
        }
        return (
          <>
            <strong>{ticker} in one line:</strong> at your {baselineLabel(e)}, EPS ends{" "}
            {usd(Math.abs(gap))} per share {gap < 0 ? "short of" : "above"} what the price needs.
          </>
        );
      },
    },
  ];
}

function shadingLine(e: Evaluation, ticker: string, targetReturn: number): ReactNode {
  const p = pathsOf(e);
  const gap = p.future.at(-1)?.gap.value ?? null;
  const at = e.atBaseline.value;
  const pe = e.exitPe.inUse.value?.pe ?? null;
  const target = pct(targetReturn);
  if (gap === null || at === null) {
    return null;
  }
  const name = baselineLabel(e);
  const g = pct(e.baselines.growth.value);
  if (gap < 0) {
    return (
      <>
        <strong>Short for {ticker}:</strong> at your {name} of {g}, EPS ends <b>{usd(-gap)}</b> per
        share short of what the price needs. In plain terms, if growth stays at that pace and the
        market pays {times(pe)} when you sell, you earn about <b>{signedPct(at.yearlyReturn)}</b> a
        year instead of your {target}. <strong>Take-away:</strong> for your target, the company must
        grow faster than you assume, or the market must pay a higher P/E than {times(pe)}.
      </>
    );
  }
  return (
    <>
      <strong>Ahead for {ticker}:</strong> at your {name} of {g}, EPS ends <b>{usd(gap)}</b> per
      share above what the price needs. In plain terms, if growth stays at that pace and the market
      pays {times(pe)} when you sell, you earn about <b>{signedPct(at.yearlyReturn)}</b> a year,
      more than your {target} target. <strong>Take-away:</strong> the price does not ask for more
      growth than you expect, which leaves room for things to go a bit worse.
    </>
  );
}

function colorLine(e: Evaluation): ReactNode {
  const p = pathsOf(e);
  const flip = e.atBaseline.value?.exitPeNeeded.value ?? null;
  const r = e.required.growth.value;
  const g = e.baselines.growth.value;
  const pe = e.exitPe.inUse.value?.pe ?? null;
  if (flip === null || r === null || g === null) {
    return null;
  }
  const good = p.gapTone.value === "good";
  const dq = p.history.at(-1)?.ttmYoy.value ?? null;
  const lead = (
    <>
      The color flips at a <b>{times(flip)}</b> exit P/E (yours is {times(pe)}) or at{" "}
      <b>{pct(r)}</b> growth (yours is {pct(g)}).{" "}
    </>
  );
  if (good) {
    return (
      <>
        {lead}So it holds unless the market pays less than {times(flip)} or growth drops below{" "}
        {pct(r)}. Ask why the market is more cautious than you.
      </>
    );
  }
  return (
    <>
      {lead}
      {dq !== null && dq < 0 ? (
        <>
          Earnings fell <b>{pct(-dq)}</b> over the last year, so part of the gap may come from
          temporarily low earnings: a recovery would close it fast.{" "}
        </>
      ) : (
        <>
          So the price assumes either faster growth than {pct(g)} or a P/E of at least {times(flip)}{" "}
          when you sell.{" "}
        </>
      )}
      Ask what the market expects that the numbers do not show yet.
    </>
  );
}
