"use client";

import type { ReactNode } from "react";
import { pct, signedPct, times, usd } from "@/components/format";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStepContext } from "@/components/walkthrough/steps";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import type { Demanding } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";
import { endLabels, linePath, useColumns, YAxis } from "@/components/charts/smallChart";
import { TipRow } from "@/components/tip/TipRow";
import { FromLevers } from "@/components/Sourced";
import { colorVar } from "@/components/Swatch";
import { Panel } from "../Panel";
import { demandingSteps } from "./steps";

const W = 520;
const H = 270;
const x0 = 44;
const x1 = W - 56;
const y0 = 14;
const y1 = H - 30;

const ink = "var(--ink)";
const accent = "var(--accent)";

function hasRequired(d: Demanding): boolean {
  return d.points.some((p) => p.required.value !== null);
}

/** missingText says why no quarter has a required growth. */
function missingText(d: Demanding): string {
  const reasons = new Set(d.points.map((p) => ("reason" in p.required ? p.required.reason : null)));
  if (reasons.size === 1 && reasons.has("dividends_reach_target")) {
    return "Your dividends alone reach the target return, so no EPS growth is required.";
  }
  if (reasons.size === 1 && reasons.has("needs_positive_eps")) {
    return "Needs positive EPS.";
  }
  return "The required growth is not available for these inputs.";
}

/** HowDemandingPanel shows the growth the price required at each past quarter, with today's levers, against the growth the company delivered. */
export function HowDemandingPanel() {
  const { evaluation: ev, provenance } = useEvaluation();
  const d = ev.howDemanding;
  const first = d.points[0];
  const today = d.points.at(-1);

  const steps = demandingSteps(useStepContext());

  return (
    <Panel
      id="how-demanding"
      className="chart"
      title="How demanding over time"
      sub={
        <>
          Required growth recomputed at each past quarter&apos;s price and TTM EPS, using
          today&apos;s levers.
          <InfoTip label="About how demanding over time">
            When the required-growth line falls, the stock got less demanding to own, because
            earnings grew faster than the price. The delivered-growth line is TTM EPS compared with
            four quarters earlier. Hover the chart for the inputs at each date.
          </InfoTip>
        </>
      }
    >
      <div className="keys">
        <span style={colorVar(accent)}>Required EPS growth</span>
        <span style={colorVar(ink)}>Delivered TTM EPS growth, YoY</span>
      </div>
      {hasRequired(d) ? (
        <>
          <DemandingChart d={d} />
          <p className="note">
            Required growth went from {pct(first?.required.value)} in {first?.label} to{" "}
            {pct(today?.required.value)} today, at a {times(ev.exitPe.inUse.value?.pe)} exit. When
            the required line sits above the delivered line, the price asks for more than the
            business recently delivered. Today uses the price of{" "}
            <FromLevers provenance={provenance.price}>{usd(today?.price.value)}</FromLevers> and the
            TTM EPS of{" "}
            <FromLevers provenance={provenance.ttmEps}>{usd(today?.ttmEps.value)}</FromLevers>.
          </p>
        </>
      ) : (
        <p className="note">{missingText(d)}</p>
      )}
      <Walkthrough
        id="how-demanding"
        name="how demanding the stock has been"
        launcher="Walk me through how demanding the stock has been"
        steps={steps}
        watchOut="A lower requirement does not make a stock cheap by itself, because earnings can fall later. Each point pairs the quarter-end price with the TTM EPS of that quarter, which the company reported some weeks later. The chart looks back: it is not a record of what a buyer knew on that date."
      />
    </Panel>
  );
}

function axisLabel(label: string, i: number, last: number): string | null {
  if (i === 0 || i === last) {
    return label;
  }
  return label.startsWith("Q1") && i > 1 && i < last - 1 ? label : null;
}

function DemandingChart({ d }: { d: Demanding }) {
  const { points, scale } = d;
  const lastIndex = points.length - 1;
  const tot = Math.max(1, lastIndex);
  const X = (i: number) => x0 + (i / tot) * (x1 - x0);
  const Y = (v: number) => y1 - ((v - scale.min) / (scale.max - scale.min)) * (y1 - y0);

  const tipFor = (i: number): ReactNode => {
    const p = points[i];
    if (p === undefined) {
      return null;
    }
    return (
      <>
        <b>
          {p.label}
          {p.today ? " (today)" : ""}
        </b>
        <TipRow label="Price" value={usd(p.price.value)} />
        <TipRow label="TTM EPS" value={usd(p.ttmEps.value)} />
        <TipRow label="P/E" value={times(p.pe.value)} />
        <TipRow label="Required growth" value={pct(p.required.value)} />
        {p.delivered.value !== null && (
          <TipRow label="Delivered, YoY" value={signedPct(p.delivered.value)} />
        )}
        <div className="tm">Required growth if you had bought here, with today&apos;s levers.</div>
      </>
    );
  };
  const { chartProps, layer, live } = useColumns(
    { count: points.length, x: X, width: (x1 - x0) / tot, y0, y1 },
    tipFor,
    d,
  );

  const rp = points.map((p, i) =>
    p.required.value === null ? null : ([X(i), Y(p.required.value)] as const),
  );
  const dp = points.map((p, i) =>
    p.delivered.value === null ? null : ([X(i), Y(p.delivered.value)] as const),
  );
  // Today's labels belong to the last quarter only; an earlier value must not stand in for it.
  const lastReq = lastIndex;
  const lastDel = lastIndex;
  const reqEnd = rp[lastReq] ?? null;
  const delEnd = dp[lastDel] ?? null;
  const [ya, yb] = endLabels(reqEnd?.[1] ?? 0, delEnd?.[1] ?? 0);
  const peak = d.peak.value;
  const peakAt = peak !== null && peak.index !== lastIndex ? (rp[peak.index] ?? null) : null;
  const today = points.at(-1);

  const label =
    `Required EPS growth by quarter from ${points[0]?.label ?? ""} to today, ` +
    `against the delivered TTM EPS growth. Today the price requires ${pct(today?.required.value)} ` +
    `a year; over the last year TTM EPS grew ${signedPct(today?.delivered.value)}.`;

  return (
    <>
      <svg viewBox={`0 0 ${String(W)} ${String(H)}`} role="img" aria-label={label} {...chartProps}>
        <YAxis
          axis={scale}
          y={Y}
          x0={x0}
          x1={x1}
          format={(v) => `${String(Math.round(v * 100))}%`}
        />
        {points.map((p, i) => {
          const text = axisLabel(p.label, i, lastIndex);
          return (
            text !== null && (
              <text key={i} x={X(i)} y={y1 + 16} textAnchor="middle">
                {text}
              </text>
            )
          );
        })}
        {scale.min < 0 && <line x1={x0} y1={Y(0)} x2={x1} y2={Y(0)} stroke="var(--muted)" />}
        <path data-walk="req" d={linePath(rp)} fill="none" stroke={accent} strokeWidth={2.2} />
        <path data-walk="del" d={linePath(dp)} fill="none" stroke={ink} strokeWidth={2} />
        {rp.map(
          (p, i) =>
            p !== null && (
              <circle
                key={i}
                data-walk={i === lastReq ? "req last" : "req"}
                cx={p[0]}
                cy={p[1]}
                r={i === lastReq ? 4 : 2.6}
                fill={accent}
              />
            ),
        )}
        {dp.map(
          (p, i) =>
            p !== null && <circle key={i} data-walk="del" cx={p[0]} cy={p[1]} r={2.4} fill={ink} />,
        )}
        {peakAt !== null && (
          <g data-walk="peak">
            <circle cx={peakAt[0]} cy={peakAt[1]} r={6.5} fill="none" stroke={accent} />
            <text x={peakAt[0]} y={peakAt[1] - 10} textAnchor="middle" style={{ fill: accent }}>
              peak
            </text>
          </g>
        )}
        {reqEnd !== null && (
          <text data-walk="req last" x={x1 + 6} y={ya} style={{ fill: accent, fontWeight: 500 }}>
            {pct(points[lastReq]?.required.value)}
          </text>
        )}
        {delEnd !== null && (
          <text data-walk="del" x={x1 + 6} y={yb} style={{ fill: ink }}>
            {pct(points[lastDel]?.delivered.value)}
          </text>
        )}
        {layer}
      </svg>
      {live}
    </>
  );
}
