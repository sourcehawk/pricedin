"use client";

import type { ReactNode } from "react";
import { pct, signedPct, times, usd, years } from "@/components/format";
import { NumberField } from "@/components/rail/NumberField";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough, type WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import {
  maxCustomExitPe,
  minCustomExitPe,
  type Evaluation,
  type ScenarioCase,
  type ScenarioOutcome,
} from "@/engine";
import { useEvaluation, useStock } from "@/state/StockProvider";
import { Panel } from "../Panel";

// The bounds of a case's growth, in percent: the engine has no result at
// -100% or lower, or above 1000%.
const minGrowthPct = -99.5;
const maxGrowthPct = 1000;

const pills: Record<string, string> = {
  good: "Beats target",
  warn: "Near target",
  bad: "Misses target",
};

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

/** percentText shows a growth fraction as the percent the case editor holds: 0.105 is "10.5". */
function percentText(g: number): string {
  return String(Number((g * 100).toFixed(2)));
}

/** joinNames lists names as a sentence does: "Bear", "Bear and Bull", "Bear, Base and Bull". */
function joinNames(names: readonly string[]): string {
  if (names.length <= 1) {
    return names.join("");
  }
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}`;
}

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

/** ScenariosPanel shows the user's bear, base and bull cases, each editable, with its returns. */
export function ScenariosPanel() {
  const { evaluation: ev, levers, profile } = useEvaluation();
  const { state, dispatch } = useStock();
  const ticker = profile.ticker.symbol;
  const n = levers.holdingYears;
  const target = levers.targetReturn;
  const cases = ev.scenarios.cases;

  const edit = (i: number, change: Partial<ScenarioCase>) => {
    // The state can be ahead of the evaluation while the user types.
    const current: ScenarioCase[] =
      state.scenarios ?? cases.map((c) => ({ name: c.name, growth: c.growth, exitPe: c.exitPe }));
    dispatch({
      type: "setScenarios",
      cases: current.map((c, k) => (k === i ? { ...c, ...change } : c)),
    });
  };

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

  const steps: WalkthroughStep[] = [
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

  return (
    <Panel
      id="scenarios"
      title="Bear, base, bull"
      sub="Your own three cases over the same holding period. No probabilities are assigned."
    >
      <div className="scen" data-walk="cases">
        {cases.map((c, i) => (
          <CaseCard
            key={c.name}
            index={i}
            outcome={c}
            years={n}
            onGrowth={(v) => {
              edit(i, { growth: clamp(v, minGrowthPct, maxGrowthPct) / 100 });
            }}
            onExitPe={(v) => {
              edit(i, { exitPe: clamp(v, minCustomExitPe, maxCustomExitPe) });
            }}
          />
        ))}
      </div>
      <Walkthrough
        id="scenarios"
        name="the bear, base and bull cases"
        launcher="Walk me through the three cases with my numbers"
        steps={steps}
        intro="Three cases help you see a range of outcomes instead of one guess. Bear is a bad outcome, base is the expected one and bull is a good one."
        watchOut="No probabilities are assigned. The three cases do not say which one is likely, so weigh them with what you learn about the business."
      />
    </Panel>
  );
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

function CaseCard({
  index,
  outcome,
  years: n,
  onGrowth,
  onExitPe,
}: {
  index: number;
  outcome: ScenarioOutcome;
  years: number;
  onGrowth: (pct: number) => void;
  onExitPe: (pe: number) => void;
}) {
  const r = outcome.result.value;
  const id = `scenario-${String(index)}`;
  return (
    <div
      className="sc"
      role="group"
      aria-labelledby={`${id}-name`}
      data-walk={`case-${String(index)}`}
    >
      <h3>
        <span id={`${id}-name`}>{outcome.name}</span>
        {r !== null && <span className={`pill ${r.tone}`}>{pills[r.tone]}</span>}
      </h3>
      <div className="inputs">
        <div className="field">
          <label htmlFor={`${id}-growth`}>EPS growth %/yr</label>
          <NumberField
            id={`${id}-growth`}
            step={0.5}
            min={minGrowthPct}
            max={maxGrowthPct}
            text={percentText(outcome.growth)}
            onValue={onGrowth}
          />
        </div>
        <div className="field">
          <label htmlFor={`${id}-pe`}>Exit P/E</label>
          <NumberField
            id={`${id}-pe`}
            step={0.5}
            min={1}
            max={maxCustomExitPe}
            text={String(outcome.exitPe)}
            onValue={onExitPe}
          />
        </div>
      </div>
      <div>
        <div className="eyebrow">
          Annual return
          <InfoTip label="About scenario return">
            Compound yearly return from today&apos;s price to the scenario price, plus any dividends
            you entered, counted as cash.
          </InfoTip>
        </div>
        <div className={r === null ? "ret" : `ret ${r.meetsTarget ? "up" : "down"}`}>
          {r === null ? "n/a" : signedPct(r.yearlyReturn)}
        </div>
      </div>
      <dl className="kv">
        <dt>EPS in {n}Y</dt>
        <dd>{r === null ? "n/a" : usd(r.eps)}</dd>
        <dt>Share price</dt>
        <dd>{r === null ? "n/a" : usd(r.exitPrice)}</dd>
        <dt>Total return</dt>
        <dd>{r === null ? "n/a" : signedPct(r.totalReturn)}</dd>
      </dl>
    </div>
  );
}
