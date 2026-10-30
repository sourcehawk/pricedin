"use client";

import { useState, type ReactNode } from "react";
import { signedPct } from "@/components/format";
import { InfoTip } from "@/components/tip/InfoTip";
import { useTipHandlers } from "@/components/tip/TipProvider";
import {
  allMetrics,
  compareScale,
  type FinancialsView,
  type Metric,
  type PairCorrelation,
  type PairCorrelationV,
} from "@/engine";
import { useView } from "@/state/StockProvider";
import styles from "./Financials.module.css";
import { HoverColumns, shortLabel } from "./HoverColumns";
import { metricValue } from "./metricFormat";
import { cls, colorVar, metricInfo } from "./metrics";

/** CompareMode is what the compare chart plots: the change since the first quarter, or from each quarter to the next. */
export type CompareMode = "sinceStart" | "quarterOverQuarter";

const defaultSelection: readonly string[] = ["revenue", "operatingIncome", "eps", "dilutedShares"];

const W = 760;
const H = 250;
const x0 = 46;
const x1 = W - 14;
const y0 = 14;
const y1 = H - 30;

const classWords: Record<PairCorrelationV["class"], string> = {
  strongSame: "strong, same direction",
  moderateSame: "moderate, same direction",
  weak: "weak or none",
  moderateOpposite: "moderate, opposite directions",
  strongOpposite: "strong, opposite directions",
};

/**
 * CompareView puts the selected metrics on one chart, as their change
 * since the first quarter or from quarter to quarter, and shows in a table
 * how the quarter-to-quarter changes of each pair moved together.
 */
export function CompareView({ ticker, view }: { ticker: string; view: FinancialsView }) {
  const [picked, setPicked] = useView<readonly string[]>("financials.compare", defaultSelection);
  const [mode, setMode] = useView<CompareMode>("financials.compareMode", "sinceStart");
  const [hover, setHover] = useState<number | null>(null);
  const selected = allMetrics().filter((m) => picked.includes(m));
  const quarters = view.quarters;
  const n = quarters.length;
  const first = quarters[0]?.label ?? "";
  const since = mode === "sinceStart";

  const toggle = (m: Metric) => {
    setPicked(selected.includes(m) ? selected.filter((x) => x !== m) : [...selected, m]);
  };

  const bar = (
    <div className="cmpbar">
      <div className="chips" style={{ margin: 0 }} role="group" aria-label="Metrics to compare">
        {allMetrics().map((m) => (
          <button
            key={m}
            type="button"
            aria-pressed={selected.includes(m)}
            onClick={() => {
              toggle(m);
            }}
          >
            <span className="sw" style={colorVar(metricInfo[m].color)} />
            {metricInfo[m].name}
          </button>
        ))}
      </div>
      <div className="seg mini" role="group" aria-label="Chart mode">
        <button
          type="button"
          aria-pressed={since}
          onClick={() => {
            setMode("sinceStart");
          }}
        >
          Since {shortLabel(first)}
        </button>
        <button
          type="button"
          aria-pressed={!since}
          onClick={() => {
            setMode("quarterOverQuarter");
          }}
        >
          Each quarter
        </button>
      </div>
    </div>
  );
  if (selected.length === 0) {
    return (
      <>
        {bar}
        <p className="note">Pick at least one metric.</p>
      </>
    );
  }

  const values = (m: Metric) =>
    (since ? view.compare.sinceStart : view.compare.quarterOverQuarter)[m].map((v) => v.value);
  const axis = compareScale(selected.flatMap(values));
  const colW = (x1 - x0) / Math.max(n, 1);
  const X = (i: number) => x0 + (i + 0.5) * colW;
  const Y = (v: number) => y1 - ((v - axis.min) / (axis.max - axis.min)) * (y1 - y0);
  const ticks: number[] = [];
  for (
    let v = Math.ceil(axis.min / axis.step - 1e-9) * axis.step;
    v <= axis.max + 1e-9;
    v += axis.step
  ) {
    ticks.push(v);
  }

  const quarterLabel = (i: number): string =>
    `${quarters[i]?.label ?? ""}: ${selected
      .map(
        (m) =>
          `${metricInfo[m].lower} ${signedPct(values(m)[i], 0)} (${metricValue(m, view.metrics[m].values[i] ?? null, 2)})`,
      )
      .join(", ")}`;

  const tip = (i: number): ReactNode => (
    <>
      <b>{quarters[i]?.label}</b>
      {selected.map((m) => (
        <div key={m}>
          <div className="prow">
            <span>
              <i
                className="sw"
                style={colorVar(m === "dilutedShares" ? "var(--surface)" : metricInfo[m].color)}
              />
              {metricInfo[m].name}
            </span>
            <strong>{signedPct(values(m)[i], 0)}</strong>
          </div>
          <div className="psub">{metricValue(m, view.metrics[m].values[i] ?? null, 2)}</div>
        </div>
      ))}
      <div className="tm">
        {since
          ? `Total change since ${first}. Lines that end higher grew more over the whole period.`
          : "Change from the quarter just before. Seasonal businesses swing up and down here every year, so compare the lines with each other, not with zero."}
      </div>
    </>
  );

  return (
    <>
      {bar}
      <p className={cls("small muted", styles.lede)}>
        {since
          ? `How much each metric changed since ${first}, in total. Read the end of each line: higher means more growth over the period.`
          : "How much each metric changed from the quarter just before. Use it to see which quarters were strong or weak, and whether the metrics rose and fell together."}
      </p>
      <svg
        viewBox={`0 0 ${String(W)} ${String(H)}`}
        role="group"
        aria-label={`Financial metrics compared by quarter for ${ticker}: ${selected
          .map((m) => `${metricInfo[m].lower} ${signedPct(values(m).at(-1), 0)}`)
          .join(", ")} ${since ? `since ${first}` : "in the latest quarter"}`}
      >
        {ticks.map((v) => (
          <g key={v}>
            <line x1={x0} y1={Y(v)} x2={x1} y2={Y(v)} stroke="var(--line)" />
            <text x={x0 - 6} y={Y(v) + 3.5} textAnchor="end">
              {Math.round(v * 100)}%
            </text>
          </g>
        ))}
        <line x1={x0} y1={Y(0)} x2={x1} y2={Y(0)} stroke="var(--muted)" strokeWidth={1.2} />
        {quarters.map((q, i) => (
          <text key={q.periodEnd} x={X(i)} y={y1 + 17} textAnchor="middle">
            {shortLabel(q.label)}
          </text>
        ))}
        <HoverColumns
          count={n}
          left={(i) => X(i) - colW / 2}
          width={colW}
          top={y0}
          height={y1 - y0}
          tip={tip}
          label={quarterLabel}
          onHover={setHover}
        />
        {hover !== null && (
          <line
            className="col-guide"
            x1={X(hover)}
            x2={X(hover)}
            y1={y0}
            y2={y1}
            stroke="var(--muted)"
            strokeDasharray="3 3"
            pointerEvents="none"
          />
        )}
        {selected.map((m) => {
          const pts = values(m)
            .map((v, i) => (v === null ? null : ([X(i), Y(v)] as const)))
            .filter((p) => p !== null);
          const c = metricInfo[m].color;
          return (
            <g key={m} pointerEvents="none">
              <path
                d={pts
                  .map((p, i) => `${i === 0 ? "M" : "L"}${p[0].toFixed(1)},${p[1].toFixed(1)}`)
                  .join("")}
                fill="none"
                stroke={c}
                strokeWidth={2}
              />
              {pts.map((p, i) => (
                <circle key={i} cx={p[0]} cy={p[1]} r={3} fill={c} />
              ))}
            </g>
          );
        })}
      </svg>
      {selected.length > 1 && <CorrelationTable view={view} selected={selected} />}
    </>
  );
}

function pairOf(
  all: readonly PairCorrelation[],
  a: Metric,
  b: Metric,
): PairCorrelation | undefined {
  return all.find((p) => (p.a === a && p.b === b) || (p.a === b && p.b === a));
}

function CorrelationTable({
  view,
  selected,
}: {
  view: FinancialsView;
  selected: readonly Metric[];
}) {
  return (
    <div className="corr-wrap">
      <table className="corr">
        <caption className={styles.caption}>
          Do they move together from quarter to quarter?
          <InfoTip label="About this table">
            <b>How to read this table</b>
            Each cell is the correlation between the changes of two metrics from one quarter to the
            next, on a scale from −1 to +1. Near +1: when one grew faster, the other did too. Near
            −1: when one grew, the other shrank. Near 0: no link. It uses only{" "}
            {view.compare.changes} quarter-to-quarter changes, so read it as a hint.
          </InfoTip>
        </caption>
        <thead>
          <tr>
            <th scope="col">
              <span className={styles.srOnly}>Metric</span>
            </th>
            {selected.map((m) => (
              <th key={m} scope="col">
                {metricInfo[m].name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {selected.map((a) => (
            <tr key={a}>
              <th scope="row">{metricInfo[a].name}</th>
              {selected.map((b) =>
                a === b ? (
                  <td key={b} className="self">
                    ·
                  </td>
                ) : (
                  <CorrelationCell
                    key={b}
                    a={a}
                    b={b}
                    pair={pairOf(view.compare.correlations, a, b)}
                  />
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CorrelationCell({
  a,
  b,
  pair,
}: {
  a: Metric;
  b: Metric;
  pair: PairCorrelation | undefined;
}) {
  const r = pair?.result.value ?? null;
  const an = metricInfo[a].name;
  const bn = metricInfo[b].name;
  const al = metricInfo[a].lower;
  const bl = metricInfo[b].lower;
  const handlers = useTipHandlers(() => (
    <>
      <b>
        {an} and {bn}
      </b>
      {r === null
        ? "Correlation n/a: one of them did not change, or a change is missing."
        : `Correlation ${r.correlation.toFixed(2)}: ${classWords[r.class]}.` +
          (r.correlation >= 0.3
            ? ` In quarters when ${al} grew faster, ${bl} usually did too.`
            : r.correlation <= -0.3
              ? ` In quarters when ${al} grew, ${bl} usually shrank or grew less.`
              : " Their quarter-to-quarter changes were not linked.")}
    </>
  ));
  const strength = r === null ? 0 : Math.round(Math.min(Math.abs(r.correlation), 1) * 55);
  const c = r !== null && r.correlation < 0 ? "var(--warn)" : "var(--accent)";
  return (
    <td
      className={styles.cell}
      tabIndex={0}
      style={{ background: `color-mix(in srgb, ${c} ${String(strength)}%, var(--surface))` }}
      {...handlers}
    >
      {r === null ? "n/a" : r.correlation.toFixed(2)}
    </td>
  );
}
