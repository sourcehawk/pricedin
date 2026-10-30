import type { ReactNode } from "react";
import { joinNames } from "@/components/copy";
import { pct, signedPct, times, usd, years } from "@/components/format";
import type { StepContext } from "@/components/walkthrough/steps";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Evaluation } from "@/engine";

const bodies: ReactNode[] = [
  <p key="bear">
    A bad but plausible outcome: growth slows further and the P/E drops, for example toward the
    lower P/E bands in the price chart. By default it uses 6 points less growth than the base case
    and a 25% lower P/E. Type your own numbers into the card.
  </p>,
  <p key="base">
    What you expect: by default the weighted trend and your exit P/E. Change it to your own view of
    the company.
  </p>,
  <p key="bull">
    A good outcome: growth picks up and investors pay more. By default it uses 6 points more growth
    than the base case and a 20% higher P/E.
  </p>,
];

/** scenarioSteps are the walkthrough steps of the bear, base and bull cases. */
export function scenarioSteps({ ticker, levers }: StepContext): WalkthroughStep[] {
  const n = levers.holdingYears;
  const target = levers.targetReturn;

  const caseLine = (e: Evaluation, i: number): ReactNode => {
    const c = e.scenarios.cases[i];
    const r = c?.result.value ?? null;
    if (c === undefined || r === null) {
      return null;
    }
    return (
      <>
        {c.name}: <b>{pct(c.growth)}</b> growth a year and a <b>{times(c.exitPe)}</b> exit P/E.{" "}
        {usd(levers.price)} becomes <b>{usd(r.exitPrice)}</b> in {years(n)},{" "}
        <b>{signedPct(r.yearlyReturn)}</b> a year, {r.meetsTarget ? "above" : "below"} your{" "}
        {pct(target)} target.
      </>
    );
  };

  return [
    ...["The bear case", "The base case", "The bull case"].map((title, i): WalkthroughStep => ({
      title,
      body: bodies[i],
      highlight: [`case-${String(i)}`],
      you: (e) => caseLine(e, i),
    })),
    {
      title: "Read the three together",
      body: (
        <p>
          If only the bull case beats your target, the stock needs things to go well. If even the
          bear case comes close, the price leaves room for mistakes.
        </p>
      ),
      highlight: ["cases"],
      levers: ["targetReturn"],
      you: (e) => {
        const s = e.scenarios;
        const meeting = s.meetingTarget;
        if (s.cases.every((c) => c.result.value === null)) {
          return <>No case has a return: {noReturnReason(e)}.</>;
        }
        return (
          <>
            Returns a year:{" "}
            <b>
              {s.cases
                .map((c) => `${c.name} ${signedPct(c.result.value?.yearlyReturn)}`)
                .join(", ")}
            </b>
            .{" "}
            {meeting.length === 0
              ? "None beats your target."
              : `${joinNames(meeting)} ${meeting.length > 1 ? "beat" : "beats"} your target.`}
          </>
        );
      },
    },
    {
      title: "Size the downside",
      body: (
        <p>
          Look at the bear case&apos;s total return. That is the loss to plan for if things go
          badly.
        </p>
      ),
      highlight: ["case-0"],
      you: (e) => {
        const r = e.scenarios.cases[0]?.result.value ?? null;
        if (r === null) {
          return null;
        }
        return (
          <>
            If the bear case plays out, {usd(levers.price)} becomes <b>{usd(r.exitPrice)}</b> in{" "}
            {years(n)}: <b>{signedPct(r.totalReturn)}</b> in total.
          </>
        );
      },
      takeaway: (e) => {
        const worst = e.scenarios.worstYearlyReturn.value;
        return (
          <>
            <strong>{ticker} in one line:</strong> {verdictLine(e)}
            {worst !== null && <> Worst case: {signedPct(worst)} a year.</>}
          </>
        );
      },
    },
  ];
}

/** noReturnReason says why the cases have no return, as a lower-case clause. */
function noReturnReason(e: Evaluation): string {
  const first = e.scenarios.cases[0]?.result;
  return first !== undefined && "reason" in first && first.reason === "needs_positive_eps"
    ? "returns need positive EPS"
    : "the inputs give none";
}

/** verdictLine names the cases that meet the target, the ones that do not, and the ones without a return. */
function verdictLine(e: Evaluation): string {
  const cases = e.scenarios.cases;
  const meeting = e.scenarios.meetingTarget;
  const missing = cases.filter((c) => c.result.value === null).map((c) => c.name);
  if (missing.length === cases.length) {
    return `no case has a return, because ${noReturnReason(e)}.`;
  }
  const others = cases
    .filter((c) => c.result.value !== null && !meeting.includes(c.name))
    .map((c) => c.name);
  const tail =
    missing.length === 0
      ? ""
      : ` ${joinNames(missing)} ${missing.length > 1 ? "have" : "has"} no return.`;
  if (missing.length > 0 && others.length === 0) {
    return `${joinNames(meeting)} ${meeting.length > 1 ? "beat" : "beats"} your target.${tail}`;
  }
  return verdictOf(meeting, others) + tail;
}

function verdictOf(meeting: readonly string[], others: readonly string[]): string {
  if (others.length === 0) {
    return "every case beats your target, so the price leaves room for mistakes.";
  }
  if (meeting.length === 0) {
    return "no case beats your target, so the price asks for more than even the best case delivers.";
  }
  return (
    `${joinNames(meeting)} ${meeting.length > 1 ? "beat" : "beats"} your target, ` +
    `but ${joinNames(others)} ${others.length > 1 ? "do not" : "does not"}.`
  );
}
