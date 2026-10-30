"use client";

import type { ReactNode } from "react";
import { pct, signedPct, times, usd } from "@/components/format";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough, type WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Demanding, Evaluation } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";
import { Panel } from "../Panel";
import {
  endLabels,
  FromLevers,
  linePath,
  swatch,
  TipRow,
  useColumns,
  YAxis,
} from "../earnings-trajectory/smallChart";

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

/** HowDemandingPanel shows the growth the price required at each past quarter, with today's levers, against the growth the company delivered. */
export function HowDemandingPanel() {
  const { evaluation: ev, profile, provenance } = useEvaluation();
  const ticker = profile.ticker.symbol;
  const d = ev.howDemanding;
  const first = d.points[0];
  const today = d.points.at(-1);

  const steps: WalkthroughStep[] = [
    {
      title: (
        <>
          <span className="lg" style={swatch(accent)} /> The required growth line
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
          <span className="lg" style={swatch(ink)} /> The delivered growth line
        </>
      ),
      body: (
        <p>
          Each point is how much TTM EPS (earnings per share over the last twelve months) actually
          grew over the year before that date. It is what the business had just shown when you would
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
        const points = (Math.abs(b - a) * 100).toFixed(1);
        return b > a ? (
          <>
            The required growth rose by <b>{points} points</b>, so the stock got more demanding: the
            price ran ahead of earnings.
          </>
        ) : (
          <>
            The required growth fell by <b>{points} points</b>, so the stock got easier to own.
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
        <span style={swatch(accent)}>Required EPS growth</span>
        <span style={swatch(ink)}>Delivered TTM EPS growth, YoY</span>
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
        <p className="note">Needs positive EPS.</p>
      )}
      <Walkthrough
        id="how-demanding"
        name="how demanding the stock has been"
        launcher="Walk me through how demanding the stock has been"
        steps={steps}
        watchOut="A lower requirement does not make a stock cheap by itself. Earnings can fall later, and each point uses only the earnings known on that date."
      />
    </Panel>
  );
}

function todayLine(e: Evaluation): ReactNode {
  const d = e.howDemanding;
  const last = d.points.at(-1)?.required.value ?? null;
  const peak = d.peak.value;
  if (last === null || peak === null) {
    return null;
  }
  if (peak.index === d.points.length - 1) {
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
  const { chartProps, layer } = useColumns(
    { count: points.length, x: X, width: (x1 - x0) / tot, y0, y1 },
    tipFor,
  );

  const rp = points.map((p, i) =>
    p.required.value === null ? null : ([X(i), Y(p.required.value)] as const),
  );
  const dp = points.map((p, i) =>
    p.delivered.value === null ? null : ([X(i), Y(p.delivered.value)] as const),
  );
  const lastReq = rp.findLastIndex((p) => p !== null);
  const lastDel = dp.findLastIndex((p) => p !== null);
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
    <svg viewBox={`0 0 ${String(W)} ${String(H)}`} role="img" aria-label={label} {...chartProps}>
      <YAxis axis={scale} y={Y} x0={x0} x1={x1} format={(v) => `${String(Math.round(v * 100))}%`} />
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
  );
}
