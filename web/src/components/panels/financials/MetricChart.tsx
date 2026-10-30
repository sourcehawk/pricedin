"use client";

import type { FocusEvent, PointerEvent, ReactNode } from "react";
import type { SourcedNumber } from "@/api/types";
import { linePath, shortLabel, useColumns } from "@/components/charts/smallChart";
import { pct, signedPct } from "@/components/format";
import { filingLink, newestFiling, SourceTip } from "@/components/Sourced";
import { TipRow } from "@/components/tip/TipRow";
import { useTipHandlers } from "@/components/tip/TipProvider";
import type { FinancialsView, Metric, Range } from "@/engine";
import styles from "./Financials.module.css";
import { cls, metricInfo, metricValue, toneClass } from "./metrics";

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
  const scale = view.metrics[metric].scale.value;
  if (scale === null || view.quarters.length === 0) {
    return <p className="note">No {metricInfo[metric].lower} in the filings of these quarters.</p>;
  }
  return <MetricPlot ticker={ticker} view={view} metric={metric} sources={sources} scale={scale} />;
}

function MetricPlot({
  ticker,
  view,
  metric,
  sources,
  scale,
}: {
  ticker: string;
  view: FinancialsView;
  metric: Metric;
  sources: readonly (SourcedNumber | null)[];
  scale: Range;
}) {
  const info = metricInfo[metric];
  const series = view.metrics[metric];
  const quarters = view.quarters;
  const n = quarters.length;
  const first = quarters[0]?.label ?? "";
  const last = quarters.at(-1)?.label ?? "";
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
        <TipRow label="Value" value={fmt(v)} />
        {yoy !== null && <TipRow label={<>vs {quarters[i - 4]?.label}</>} value={signedPct(yoy)} />}
        {shares && qoq !== null && <TipRow label="vs previous quarter" value={signedPct(qoq)} />}
        {metric === "operatingIncome" && (
          <TipRow label="Margin" value={pct(view.operatingMargin[i]?.value)} />
        )}
        {metric === "operatingCosts" && (
          <TipRow label="Share of revenue" value={pct(view.costShareOfRevenue[i]?.value)} />
        )}
        {s !== null && (
          <div className="tm">
            <SourceTip value={s} />
          </div>
        )}
      </>
    );
  };
  const { chartProps, layer, live, guide } = useColumns(
    { count: n, x: cx, width: bw, y0: y0 - 8, y1 },
    tip,
    series,
  );

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
        onHover={(on) => {
          guide(on ? i : null);
        }}
      />,
    );
  });

  const points = quarters.map((_, i) => {
    const v = series.values[i] ?? null;
    return v === null ? null : ([cx(i), Y(v)] as const);
  });

  return (
    <>
      <svg
        viewBox={`0 0 ${String(W)} ${String(H)}`}
        role="group"
        aria-label={`Quarterly ${info.lower} of ${ticker}, ${first} to ${last}`}
        {...chartProps}
      >
        {layer}
        {!shares && <line x1={x0} y1={zero} x2={x1} y2={zero} stroke="var(--line)" />}
        {shares && (
          <>
            <path
              d={linePath(points)}
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
      {live}
    </>
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
  onHover,
}: {
  x: number;
  y: number;
  text: string;
  /** The accessible name, with the quarter and the metric. */
  name: string;
  sourced: SourcedNumber | null;
  tip: () => ReactNode;
  /** Gets true while the value has the pointer or the focus, so the chart keeps the quarter's guide line. */
  onHover: (on: boolean) => void;
}) {
  const tipHandlers = useTipHandlers(tip);
  const handlers = {
    ...tipHandlers,
    onPointerEnter: (e: PointerEvent) => {
      if (e.pointerType !== "touch") {
        onHover(true);
      }
      tipHandlers.onPointerEnter(e);
    },
    onPointerLeave: (e: PointerEvent) => {
      if (e.pointerType !== "touch") {
        onHover(false);
      }
      tipHandlers.onPointerLeave(e);
    },
    onFocus: (e: FocusEvent) => {
      onHover(true);
      tipHandlers.onFocus(e);
    },
    onBlur: (e: FocusEvent) => {
      onHover(false);
      tipHandlers.onBlur(e);
    },
  };
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
