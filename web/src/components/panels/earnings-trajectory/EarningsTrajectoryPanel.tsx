"use client";

import type { ReactNode } from "react";
import { signedPct, signedUsd, usd, years } from "@/components/format";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStepContext } from "@/components/walkthrough/steps";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import type { Paths } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { endLabels, linePath, useColumns, YAxis } from "@/components/charts/smallChart";
import { TipRow } from "@/components/tip/TipRow";
import { FromLevers } from "@/components/Sourced";
import { colorVar } from "@/components/Swatch";
import { trajectorySteps } from "./steps";

const W = 520;
const H = 270;
const x0 = 48;
const x1 = W - 66;
const y0 = 14;
const y1 = H - 30;

const ink = "var(--ink)";
const muted = "var(--muted)";
const accent = "var(--accent)";

function yearsOut(k: number): string {
  if (k === 0) {
    return "today";
  }
  return `${String(k / 4)} ${k === 4 ? "year" : "years"} out`;
}

/** EarningsTrajectoryPanel shows TTM EPS by quarter, then the baseline path and the path the price requires. */
export function EarningsTrajectoryPanel() {
  const { evaluation: ev, levers, provenance } = useEvaluation();
  const n = levers.holdingYears;
  const steps = trajectorySteps(useStepContext());

  return (
    <Panel
      id="earnings-trajectory"
      className="chart"
      title="Earnings trajectory"
      sub={
        <>
          TTM EPS by quarter, then two paths: your baseline continuing, and the path the price
          requires.
          <InfoTip label="About shading">
            <span className="sw" style={colorVar("var(--bad)")} /> Shading: the required path ends
            above the baseline, so the price needs more growth than the trend gives.{" "}
            <span className="sw" style={colorVar("var(--good)")} /> Shading: the baseline beats it.
            Hover the chart for values.
          </InfoTip>
        </>
      }
    >
      <div className="keys">
        <span style={colorVar(ink)}>Reported TTM EPS</span>
        <span className="dash" style={colorVar(muted)}>
          Baseline continues
        </span>
        <span className="dash" style={colorVar(accent)}>
          Required path
        </span>
      </div>
      {ev.earningsPaths.value === null ? (
        <p className="note">Needs positive EPS to project.</p>
      ) : (
        <>
          <TrajectoryChart paths={ev.earningsPaths.value} years={n} />
          <p className="note">
            Projections start from today&apos;s TTM EPS of{" "}
            <FromLevers provenance={provenance.ttmEps}>{usd(levers.ttmEps)}</FromLevers> and
            compound quarterly.
          </p>
        </>
      )}
      <Walkthrough
        id="earnings-trajectory"
        name="the earnings trajectory"
        launcher="Walk me through the earnings trajectory with my numbers"
        steps={steps}
        watchOut="Only the end point of each path matters for the return. The smooth curves are one way to get there. Real earnings arrive in uneven steps."
      />
    </Panel>
  );
}

function TrajectoryChart({ paths, years: n }: { paths: Paths; years: number }) {
  const { history, future, scale } = paths;
  const { history: sources } = useEvaluation();
  const known = new Map(sources.quarters.map((q) => [q.periodEnd, q.ttmEpsKnownOn]));
  const F = future.length - 1;
  // Without reported quarters, the projection still starts at column 0.
  const today = Math.max(0, history.length - 1);
  const tot = Math.max(1, today + F);
  const X = (i: number) => x0 + (i / tot) * (x1 - x0);
  const Y = (v: number) => y1 - ((v - scale.min) / (scale.max - scale.min)) * (y1 - y0);
  const pt = (i: number, v: number | null): readonly [number, number] | null =>
    v === null ? null : [X(i), Y(v)];

  const tipFor = (i: number): ReactNode => {
    if (i < history.length) {
      const q = history[i];
      if (q === undefined) {
        return null;
      }
      const on = known.get(q.periodEnd) ?? null;
      return (
        <>
          <b>
            {q.label}
            {i === today ? " (today)" : ""}
          </b>
          <TipRow label="Reported TTM EPS" value={usd(q.ttmEps.value)} />
          {q.ttmYoy.value !== null && (
            <TipRow label="vs a year earlier" value={signedPct(q.ttmYoy.value)} />
          )}
          {on !== null && <div className="tm">Known from the filings on {on}</div>}
        </>
      );
    }
    const f = future[i - today];
    if (f === undefined) {
      return null;
    }
    return (
      <>
        <b>
          {f.label === null ? "" : `${f.label} · `}
          {yearsOut(f.quartersAhead)}
        </b>
        <TipRow label="Baseline EPS" value={usd(f.baseline.value)} />
        {f.required.value !== null && (
          <>
            <TipRow label="Required EPS" value={usd(f.required.value)} />
            <TipRow label="Gap" value={signedUsd(f.gap.value)} />
          </>
        )}
      </>
    );
  };
  const { chartProps, layer, live } = useColumns(
    { count: tot + 1, x: X, width: (x1 - x0) / tot, y0, y1, start: today },
    tipFor,
    paths,
  );

  const hp = history.map((q, i) => pt(i, q.ttmEps.value));
  const bp = future.map((f, k) => pt(today + k, f.baseline.value));
  const rp = future.map((f, k) => pt(today + k, f.required.value));
  const end = future.at(-1);
  const endBase = end?.baseline.value ?? null;
  const endReq = end?.required.value ?? null;
  const tone = paths.gapTone.value;
  const shade =
    tone !== null && bp.every((p) => p !== null) && rp.every((p) => p !== null)
      ? `${linePath(bp)} L${rp
          .slice()
          .reverse()
          .map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`)
          .join(" L")} Z`
      : null;
  const xn = X(today);
  const step = scale.step;
  const decimals = step < 1 ? (step < 0.1 ? 2 : 1) : 0;
  const yearTicks = [...history.map((q) => q.label), ...future.slice(1).map((f) => f.label)];
  const [yb, yr] = endLabels(Y(endBase ?? 0), Y(endReq ?? 0));

  const first = history[0];
  const label =
    `TTM EPS by quarter from ${first?.label ?? ""} to ${history.at(-1)?.label ?? ""}, ` +
    `then two paths over ${years(n)}: the baseline path reaches ${usd(endBase)} and ` +
    (endReq === null
      ? "the required path is not available."
      : `the required path reaches ${usd(endReq)}.`);

  return (
    <>
      <svg viewBox={`0 0 ${String(W)} ${String(H)}`} role="img" aria-label={label} {...chartProps}>
        <YAxis axis={scale} y={Y} x0={x0} x1={x1} format={(v) => usd(v, decimals)} />
        <rect x={xn} y={y0} width={x1 - xn} height={y1 - y0} fill="var(--sunk)" />
        <line x1={xn} y1={y0} x2={xn} y2={y1} stroke={muted} strokeDasharray="2 3" />
        <text x={xn + 4} y={y0 + 10} textAnchor="start">
          today
        </text>
        {yearTicks.map((l, i) => {
          const m = l === null ? null : /^Q1 FY(\d{2})$/.exec(l);
          if (m === null || (tot > 30 && Number(m[1]) % 2 === 1)) {
            return null;
          }
          return (
            <text key={i} x={X(i)} y={y1 + 16} textAnchor="middle">
              FY{m[1]}
            </text>
          );
        })}
        {shade !== null && tone !== null && (
          <path
            className={`L-shade ${tone}`}
            data-walk="shade"
            d={shade}
            fill={`color-mix(in srgb, var(--${tone}) 12%, transparent)`}
          />
        )}
        <path data-walk="hist" d={linePath(hp)} fill="none" stroke={ink} strokeWidth={2.2} />
        <path
          data-walk="base"
          d={linePath(bp)}
          fill="none"
          stroke={muted}
          strokeWidth={2}
          strokeDasharray="5 4"
        />
        {endReq !== null && (
          <path
            data-walk="req"
            d={linePath(rp)}
            fill="none"
            stroke={accent}
            strokeWidth={2.2}
            strokeDasharray="5 4"
          />
        )}
        {hp.map(
          (p, i) =>
            p !== null && (
              <circle
                key={i}
                data-walk="hist"
                cx={p[0]}
                cy={p[1]}
                r={i === today ? 3.5 : 2.2}
                fill={ink}
              />
            ),
        )}
        {future.map((f, k) => {
          if (k === 0 || k % 4 !== 0) {
            return null;
          }
          const b = bp[k];
          const r = rp[k];
          return (
            <g key={k}>
              {b && <circle data-walk="base" cx={b[0]} cy={b[1]} r={2.6} fill={muted} />}
              {r && <circle data-walk="req" cx={r[0]} cy={r[1]} r={2.6} fill={accent} />}
            </g>
          );
        })}
        {endBase !== null && (
          <text data-walk="base" x={x1 + 6} y={yb} style={{ fill: muted }}>
            {usd(endBase)}
          </text>
        )}
        {endReq !== null && (
          <text data-walk="req" x={x1 + 6} y={yr} style={{ fill: accent, fontWeight: 500 }}>
            {usd(endReq)}
          </text>
        )}
        {layer}
      </svg>
      {live}
    </>
  );
}
