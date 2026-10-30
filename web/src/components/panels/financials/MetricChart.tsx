"use client";

import { useState, type ReactNode } from "react";
import type { SourcedNumber } from "@/api/types";
import { pct, signedPct } from "@/components/format";
import { filingLink, newestFiling, SourceTip } from "@/components/Sourced";
import { useTipHandlers } from "@/components/tip/TipProvider";
import type { FinancialsView, Metric } from "@/engine";
import styles from "./Financials.module.css";
import { HoverColumns, shortLabel } from "./HoverColumns";
import { metricValue } from "./metricFormat";
import { cls, metricInfo, toneClass } from "./metrics";

const W = 760;
const H = 210;
const x0 = 8;
const x1 = W - 8;
const y0 = 18;
const y1 = H - 40;

/**
 * MetricChart shows one metric per quarter: bars from zero, or for the
 * share count a line on a zoomed scale. Each value links to its filing.
 * Under each bar is its change against a year earlier (against the quarter
 * before for the share count), in the tone the engine gives it.
 */
export function MetricChart({
  ticker,
  view,
  metric,
  sources,
}: {
  ticker: string;
  view: FinancialsView;
  metric: Metric;
  sources: readonly (SourcedNumber | null)[];
}) {
  const [hover, setHover] = useState<number | null>(null);
  const info = metricInfo[metric];
  const series = view.metrics[metric];
  const quarters = view.quarters;
  const n = quarters.length;
  const scale = series.scale.value;
  const first = quarters[0]?.label ?? "";
  const last = quarters.at(-1)?.label ?? "";
  if (scale === null || n === 0) {
    return <p className="note">No {info.lower} in the filings of these quarters.</p>;
  }
  const bw = (x1 - x0) / n;
  const cx = (i: number) => x0 + i * bw + bw / 2;
  const Y = (v: number) => y1 - ((v - scale.min) / (scale.max - scale.min)) * (y1 - y0);
  const shares = metric === "dilutedShares";
  const fmt = (v: number | null) => metricValue(metric, v);

  const tip = (i: number): ReactNode => {
    const q = quarters[i];
    const v = series.values[i] ?? null;
    const yoy = series.yoy[i]?.value ?? null;
    const qoq = series.qoq[i]?.value ?? null;
    const s = sources[i] ?? null;
    return (
      <>
        <b>
          {q?.label} {info.name}
        </b>
        <div className="row">
          <span>Value</span>
          <span>{fmt(v)}</span>
        </div>
        {yoy !== null && (
          <div className="row">
            <span>vs {quarters[i - 4]?.label}</span>
            <span>{signedPct(yoy)}</span>
          </div>
        )}
        {shares && qoq !== null && (
          <div className="row">
            <span>vs previous quarter</span>
            <span>{signedPct(qoq)}</span>
          </div>
        )}
        {metric === "operatingIncome" && (
          <div className="row">
            <span>Margin</span>
            <span>{pct(view.operatingMargin[i]?.value)}</span>
          </div>
        )}
        {metric === "operatingCosts" && (
          <div className="row">
            <span>Share of revenue</span>
            <span>{pct(view.costShareOfRevenue[i]?.value)}</span>
          </div>
        )}
        {s !== null && (
          <div className="tm">
            <SourceTip value={s} />
          </div>
        )}
      </>
    );
  };

  const marks: ReactNode[] = [];
  const zero = Y(Math.max(scale.min, Math.min(scale.max, 0)));
  quarters.forEach((q, i) => {
    const v = series.values[i] ?? null;
    marks.push(
      <text key={`q${String(i)}`} x={cx(i)} y={y1 + 15} textAnchor="middle">
        {shortLabel(q.label)}
      </text>,
    );
    if (shares) {
      const c = series.qoq[i]?.value ?? null;
      const tone = signedPct(c) === signedPct(0) ? null : (series.qoqTone[i]?.value ?? null);
      if (c !== null) {
        marks.push(
          <text
            key={`c${String(i)}`}
            x={cx(i)}
            y={y1 + 30}
            textAnchor="middle"
            className={cls(toneClass(tone), styles.toned, Math.abs(c) < 0.005 && styles.faint)}
          >
            {signedPct(c)} QoQ
          </text>,
        );
      }
    } else {
      const c = series.yoy[i]?.value ?? null;
      if (c !== null) {
        marks.push(
          <text
            key={`c${String(i)}`}
            x={cx(i)}
            y={y1 + 30}
            textAnchor="middle"
            className={cls(toneClass(series.yoyTone[i]?.value ?? null), styles.toned)}
          >
            {signedPct(c, 0)} YoY
          </text>,
        );
      }
    }
    if (v === null) {
      marks.push(
        <text key={`v${String(i)}`} x={cx(i)} y={zero - 6} textAnchor="middle">
          n/a
        </text>,
      );
      return;
    }
    const newest = i === n - 1;
    if (shares) {
      marks.push(
        <circle
          key={`d${String(i)}`}
          cx={cx(i)}
          cy={Y(v)}
          r={newest ? 5 : 4}
          fill="var(--accent)"
          stroke="var(--surface)"
          strokeWidth={1.5}
          pointerEvents="none"
        />,
      );
    } else {
      const top = Math.min(Y(v), zero);
      marks.push(
        <rect
          key={`b${String(i)}`}
          x={x0 + i * bw + bw * 0.2}
          y={top}
          width={bw * 0.6}
          height={Math.abs(Y(v) - zero)}
          rx={3}
          fill={newest ? "var(--accent)" : "color-mix(in srgb, var(--accent) 30%, var(--surface))"}
          pointerEvents="none"
        />,
      );
    }
    const labelY = shares ? Y(v) - 10 : v >= 0 ? Y(v) - 6 : Y(v) + 13;
    marks.push(
      <ValueLabel
        key={`v${String(i)}`}
        x={cx(i)}
        y={labelY}
        text={fmt(v)}
        name={`${q.label} ${info.name}: ${fmt(v)}`}
        sourced={sources[i] ?? null}
        tip={() => tip(i)}
      />,
    );
  });

  const points = quarters
    .map((_, i) => series.values[i] ?? null)
    .map((v, i) => (v === null ? null : `${cx(i).toFixed(1)},${Y(v).toFixed(1)}`))
    .filter((p): p is string => p !== null);

  return (
    <svg
      viewBox={`0 0 ${String(W)} ${String(H)}`}
      role="group"
      aria-label={`Quarterly ${info.lower} of ${ticker}, ${first} to ${last}`}
    >
      <HoverColumns
        count={n}
        left={(i) => x0 + i * bw}
        width={bw}
        top={0}
        height={H}
        tip={tip}
        onHover={setHover}
      />
      {hover !== null && (
        <line
          className="col-guide"
          x1={cx(hover)}
          x2={cx(hover)}
          y1={y0 - 8}
          y2={y1}
          stroke="var(--muted)"
          strokeDasharray="3 3"
          pointerEvents="none"
        />
      )}
      {!shares && <line x1={x0} y1={zero} x2={x1} y2={zero} stroke="var(--line)" />}
      {shares && (
        <>
          <path
            d={`M${points.join("L")}`}
            fill="none"
            stroke="var(--accent)"
            strokeWidth={2}
            pointerEvents="none"
          />
          <text x={x1} y={y0 - 4} textAnchor="end">
            Change shown vs the previous quarter (QoQ) · scale zoomed in, not from zero
          </text>
        </>
      )}
      {marks}
    </svg>
  );
}

/**
 * ValueLabel is a value over its bar. A value from a filing links to that
 * filing; hovering or focusing it shows the quarter's tooltip.
 */
function ValueLabel({
  x,
  y,
  text,
  name,
  sourced,
  tip,
}: {
  x: number;
  y: number;
  text: string;
  /** The accessible name, with the quarter and the metric. */
  name: string;
  sourced: SourcedNumber | null;
  tip: () => ReactNode;
}) {
  const handlers = useTipHandlers(tip);
  const label = (
    <text x={x} y={y} textAnchor="middle" className={styles.value}>
      {text}
    </text>
  );
  const href = sourced === null ? null : filingLink(sourced.source);
  if (sourced === null) {
    return label;
  }
  if (href === null) {
    return (
      <g tabIndex={0} aria-label={name} {...handlers}>
        {label}
      </g>
    );
  }
  const filing = newestFiling(sourced.source);
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={styles.valueLink}
      aria-label={`${name}, Form ${filing?.form ?? ""} filed ${filing?.filed ?? ""}`}
      {...handlers}
    >
      {label}
    </a>
  );
}
