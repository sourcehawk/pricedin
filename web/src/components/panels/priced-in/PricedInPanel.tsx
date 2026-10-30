"use client";

import type { ReactNode } from "react";
import type { QuarterSources, SourcedNumber } from "@/api/types";
import type { Evaluation, Verdict } from "@/engine";
import { pct, pp, signedPct, times, usd, years } from "@/components/format";
import { baselineName } from "@/components/rail/notes";
import { newestFiling, Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import { useEvaluation } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { GrowthRuler } from "./GrowthRuler";
import styles from "./PricedIn.module.css";
import { pricedInSteps } from "./steps";

/** verdictTone is the pill class of a verdict. */
export const verdictTone: Record<Verdict, "good" | "warn" | "bad"> = {
  undemanding: "good",
  tight: "warn",
  demanding: "bad",
};

function verdictText(ev: Evaluation): string | null {
  const v = ev.verdict.value;
  if (v === null) {
    return null;
  }
  if (ev.required.growth.value === null) {
    return "Undemanding: your dividends alone reach the target";
  }
  switch (v) {
    case "undemanding":
      return `Undemanding: the ${baselineName(ev)} clears the bar`;
    case "tight":
      return `Tight: needs the ${baselineName(ev)} to hold`;
    case "demanding":
      return "Demanding: needs growth to accelerate";
  }
}

/** yoySource is the YoY growth of quarter i with the sources of the two quarters it compares; null without a filing behind both. */
function yoySource(q: readonly QuarterSources[], i: number, value: number): SourcedNumber | null {
  const now = q[i]?.eps;
  const before = q[i - 4]?.eps;
  if (!now || !before) {
    return null;
  }
  const nowFiling = newestFiling(now);
  const beforeFiling = newestFiling(before);
  if (nowFiling === null || beforeFiling === null) {
    return null;
  }
  const asOf = nowFiling.filed > beforeFiling.filed ? nowFiling.filed : beforeFiling.filed;
  return {
    value,
    source: {
      kind: "derived",
      method: "derived",
      label: "EPS against the same quarter a year earlier",
      estimate: false,
      inputs: [now, before],
    },
    asOf,
  };
}

function Row({
  term,
  tip,
  walk,
  children,
}: {
  term: ReactNode;
  tip?: { label: string; text: ReactNode };
  walk?: string;
  children: ReactNode;
}) {
  return (
    <>
      <dt data-walk={walk}>
        {term}
        {tip !== undefined && <InfoTip label={tip.label}>{tip.text}</InfoTip>}
      </dt>
      <dd data-walk={walk}>{children}</dd>
    </>
  );
}

/** PricedInPanel renders only in the page's ready state: useEvaluation throws before an evaluation exists. */
export function PricedInPanel() {
  const { evaluation: ev, state, levers, history, provenance } = useEvaluation();
  const r = state.targetReturn;
  const n = state.holdingYears;
  const sym = state.ticker ?? "The stock";
  const req = ev.required;
  const R = req.growth.value;
  const pe = ev.exitPe.inUse.value?.pe ?? null;
  const ab = ev.atBaseline.value;
  const b = ev.baselines;
  const noGrowth = R === null && req.growth.reason === "dividends_reach_target";
  const verdict = ev.verdict.value;
  const tone = verdict === null ? null : verdictTone[verdict];
  const message = verdictText(ev);
  const eps = (
    <Sourced
      value={provenance.ttmEps?.kind === "stock" ? provenance.ttmEps.sourced : null}
      user={provenance.ttmEps?.kind === "user"}
    >
      {usd(levers.ttmEps)}
    </Sourced>
  );

  let sub: string;
  if (!(levers.ttmEps > 0)) {
    sub = "A P/E model needs positive EPS. Loss-making companies need a different method.";
  } else if (pe === null) {
    sub = "The exit P/E needs more price and earnings history.";
  } else {
    sub = `For ${pct(r)} a year over ${years(n)}, exiting at ${pe.toFixed(1)}× earnings${ev.exitPe.mode === "estimated" ? " (estimated)" : ""}${req.dividends > 0 ? ", with dividends" : ""}.`;
  }

  const gap = ev.gap.value;
  const last = ev.quarters.length - 1;
  const chips = ev.quarters
    .map((q, i) => ({ q, i }))
    .slice(Math.max(0, last - 5))
    .filter(({ q }) => q.yoy.value !== null);

  return (
    <Panel id="priced-in" title="Priced in" className="vpanel">
      <div className="verdict">
        <div>
          <div className="eyebrow">
            EPS growth required
            <InfoTip label="About required growth">
              {R === null ? (
                noGrowth ? (
                  <>
                    <b>No growth needed</b>Your dividends alone reach {pct(r)} a year.
                  </>
                ) : (
                  <>
                    <b>Required growth</b>The average yearly EPS growth (compound) that turns
                    today&apos;s price into your target return, if you sell at the exit P/E.
                  </>
                )
              ) : (
                <>
                  <b>What {pct(R)} means</b>
                  {sym} must grow earnings per share by {pct(R)} a year on average for {years(n)}:
                  from {usd(levers.ttmEps)} today to {usd(req.eps.value)}. At your exit P/E of{" "}
                  {times(pe)}, that is a share price of {usd(req.exitPrice.value)}
                  {req.dividends > 0 ? `, plus ${usd(req.dividends)} in dividends` : ""}. That turns
                  today&apos;s {usd(levers.price)} into {pct(r)} a year. Any path to the same end
                  EPS works: a flat year and two strong years give the same result.
                </>
              )}
            </InfoTip>
          </div>
          <div
            className="big num v-req"
            data-walk="required"
            data-testid="required-growth"
            style={{ margin: "8px 0 6px" }}
          >
            {R !== null ? pct(R) : noGrowth ? "none" : "n/a"}
            {(R !== null || noGrowth) && <small>/ yr</small>}
          </div>
          <div className="muted" data-walk="required">
            {sub}
          </div>
          {R !== null && (
            <div className="vexit" data-walk="exit">
              <div>
                <span className="eyebrow">Exit share price needed</span>
                <b>{usd(req.exitPrice.value)}</b>
                <small>
                  {signedPct(req.priceChange.value, 0)} from today, in {years(n)}
                </small>
              </div>
              <div>
                <span className="eyebrow">EPS needed</span>
                <b>{usd(req.eps.value)}</b>
                <small>
                  from {eps} today, × {times(pe).replace("×", "")} exit P/E
                </small>
              </div>
            </div>
          )}
          <div style={{ marginTop: 20, display: "flex", alignItems: "center" }}>
            {message !== null && tone !== null && (
              <span className={`pill ${tone}`} data-walk="verdict">
                {message}
              </span>
            )}
            <InfoTip label="About the verdict">
              <b>The verdict</b>
              <span className="pill good">Undemanding</span> your baseline beats the required growth
              by more than 1.5 pp. <span className="pill warn">Tight</span> within 1.5 pp either
              way. <span className="pill bad">Demanding</span> the baseline falls short by more than
              1.5 pp.
            </InfoTip>
          </div>
        </div>
        <dl className="kv">
          <Row
            walk="baselines"
            term={
              <>
                Weighted trend <span className="small">(8Q)</span>
              </>
            }
            tip={{
              label: "About weighted trend",
              text: "Year-over-year EPS growth of the last 8 quarters, averaged so the latest quarters count most. A quarter's weight halves every two quarters back, so quarters from two years ago count about an eighth as much as the latest one.",
            }}
          >
            {pct(b.weighted.value)}
          </Row>
          <Row
            walk="baselines"
            term={
              <>
                Today <span className="small">(latest quarter YoY)</span>
              </>
            }
            tip={{
              label: "About today’s growth",
              text: "The latest quarter's EPS against the same quarter a year earlier. Comparing like quarters cancels seasonality.",
            }}
          >
            {pct(b.today.value)}
          </Row>
          <Row
            walk="baselines"
            term={
              <>
                Trailing growth <span className="small">({String(b.trailing.months)} mo)</span>
              </>
            }
            tip={{
              label: "About trailing growth",
              text: "Average yearly growth of TTM EPS over the number of months set next to Trailing growth in the left panel (12 by default).",
            }}
          >
            {pct(b.trailing.growth.value)}
          </Row>
          <Row
            walk="gap"
            term={<>Gap vs {baselineName(ev)}</>}
            tip={{
              label: "About gap",
              text: "Baseline growth minus required growth, in percentage points (pp). Negative means the business must grow faster than it has been growing.",
            }}
          >
            {gap === null ? "n/a" : <span className={gap >= 0 ? "up" : "down"}>{pp(gap)}</span>}
          </Row>
          <div className="sep" />
          <Row
            term="Implied price at baseline"
            tip={{
              label: "About implied price",
              text: "TTM EPS grown at the baseline rate for the holding period, times the exit P/E.",
            }}
          >
            {usd(ab?.exitPrice)}
          </Row>
          <Row term="Annual return at baseline">
            {ab === null ? (
              "n/a"
            ) : (
              <span className={ab.meetsTarget ? "up" : "down"}>{signedPct(ab.yearlyReturn)}</span>
            )}
          </Row>
          <Row
            term="Exit P/E needed at baseline"
            tip={{
              label: "About exit P/E needed",
              text: "The multiple the market must pay at exit for you to hit your target, if EPS grows exactly at the baseline rate.",
            }}
          >
            {times(ab?.exitPeNeeded.value)}
          </Row>
        </dl>
      </div>
      <div className="yoy" data-walk="yoy">
        <span className="muted" style={{ border: 0, background: "none", paddingLeft: 0 }}>
          Quarterly EPS YoY
          <InfoTip label="About quarterly YoY">
            Each quarter&apos;s EPS against the same quarter one year earlier. A falling sequence
            means growth is slowing.
          </InfoTip>
          :
        </span>
        <ul aria-label="Quarterly EPS growth, year over year" className={styles.chips}>
          {chips.map(({ q, i }) => {
            const v = q.yoy.value ?? 0;
            const text = `${q.label} ${signedPct(v, 0)}`;
            const src = yoySource(history.quarters, i, v);
            return (
              <li key={q.label}>
                {src === null ? (
                  <span>{text}</span>
                ) : (
                  <Sourced value={src}>
                    <span>{text}</span>
                  </Sourced>
                )}
              </li>
            );
          })}
        </ul>
      </div>
      <GrowthRuler ev={ev} targetReturn={r} />
      <Walkthrough
        id="priced-in"
        name="the priced-in summary"
        launcher="Walk me through the priced-in summary with my numbers"
        intro="This panel answers one question: how fast must the company grow its earnings for you to get the return you want, and has it grown that fast before?"
        watchOut="EPS can jump for reasons that do not repeat, such as a tax benefit, an asset sale or large share buybacks. Before you trust a growth rate, check in Financials that revenue and operating income grew too."
        steps={pricedInSteps({ sym, r, n, price: levers.price, eps: levers.ttmEps })}
      />
    </Panel>
  );
}
