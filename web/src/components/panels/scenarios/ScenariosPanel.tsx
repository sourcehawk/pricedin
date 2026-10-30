"use client";

import { signedPct, usd } from "@/components/format";
import { NumberField } from "@/components/rail/NumberField";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStepContext } from "@/components/walkthrough/steps";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import {
  maxCustomExitPe,
  minCustomExitPe,
  type ScenarioCase,
  type ScenarioOutcome,
} from "@/engine";
import { useEvaluation, useStock } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { scenarioSteps } from "./steps";

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

/** ScenariosPanel shows the user's bear, base and bull cases, each editable, with its returns. */
export function ScenariosPanel() {
  const { evaluation: ev, levers } = useEvaluation();
  const { state, dispatch } = useStock();
  const n = levers.holdingYears;
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

  const steps = scenarioSteps(useStepContext());

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
