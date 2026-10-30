"use client";

import type { ReactNode } from "react";
import type { Evaluation } from "@/engine";
import { pct } from "@/components/format";
import { useTipHandlers } from "@/components/tip/TipProvider";

interface Mark {
  key: string;
  value: number;
  label: string;
  color: string;
  /** The label sits above the scale; the baselines' labels sit below it. */
  up: boolean;
  big: boolean;
  tip: ReactNode;
}

const width = 760;
const x0 = 24;
const x1 = width - 24;
const lanes = [84, 98, 112];
const labelStyle = { fontFamily: "var(--f-body)", fontWeight: 600 } as const;
const tickStyle = { fontFamily: "var(--f-mono)", fontSize: 10.5, fill: "var(--muted)" } as const;

function marksOf(ev: Evaluation, targetReturn: number): Mark[] {
  const b = ev.baselines;
  const marks: Mark[] = [];
  const req = ev.required.growth.value;
  const pe = ev.exitPe.inUse.value?.pe;
  if (req !== null) {
    marks.push({
      key: "required",
      value: req,
      label: `Required ${pct(req)}`,
      color: "var(--accent)",
      up: true,
      big: true,
      tip: (
        <>
          <b>Required: {pct(req)} a year</b>
          The average EPS growth that gets you {pct(targetReturn)} a year at a{" "}
          {pe === undefined ? "n/a" : `${pe.toFixed(1)}×`} exit. Growth in the shaded part of the
          scale beats it.
        </>
      ),
    });
  }
  if (b.weighted.value !== null) {
    marks.push({
      key: "weighted",
      value: b.weighted.value,
      label: `Weighted ${pct(b.weighted.value)}`,
      color: "var(--s3)",
      up: false,
      big: false,
      tip: (
        <>
          <b>Weighted trend: {pct(b.weighted.value)}</b>
          Year-over-year EPS growth of the last 8 quarters, with the latest quarters counting most.
        </>
      ),
    });
  }
  if (b.today.value !== null) {
    marks.push({
      key: "today",
      value: b.today.value,
      label: `Today ${pct(b.today.value)}`,
      color: "var(--ink)",
      up: false,
      big: false,
      tip: (
        <>
          <b>Today: {pct(b.today.value)}</b>
          EPS growth of the latest quarter against the same quarter a year earlier.
        </>
      ),
    });
  }
  const t = b.trailing;
  if (t.growth.value !== null) {
    const m = String(t.months);
    marks.push({
      key: "trailing",
      value: t.growth.value,
      label: `Trailing ${m}m ${pct(t.growth.value)}`,
      color: "var(--muted)",
      up: false,
      big: false,
      tip: (
        <>
          <b>
            Trailing {m} months: {pct(t.growth.value)}
          </b>
          Average yearly growth of TTM EPS over the last {m} months.
        </>
      ),
    });
  }
  if (b.active === "custom" && b.custom.value !== null) {
    marks.push({
      key: "custom",
      value: b.custom.value,
      label: `Custom ${pct(b.custom.value)}`,
      color: "var(--s4)",
      up: true,
      big: false,
      tip: (
        <>
          <b>Custom: {pct(b.custom.value)}</b>
          Your own growth assumption.
        </>
      ),
    });
  }
  return marks.sort((a, b) => a.value - b.value);
}

function MarkDot({
  cx,
  mark,
  children,
}: {
  cx: number;
  mark: Mark;
  children: ReactNode;
}) {
  const handlers = useTipHandlers(() => mark.tip);
  return (
    <g>
      <circle
        data-walk="ruler-marks"
        cx={cx}
        cy={46}
        r={mark.big ? 8 : 6}
        fill={mark.color}
        stroke="var(--surface)"
        strokeWidth={2}
      />
      {children}
      <circle
        cx={cx}
        cy={46}
        r={13}
        fill="transparent"
        {...handlers}
      />
    </g>
  );
}

/**
 * GrowthRuler is the growth scale of the priced-in panel: the required
 * growth and each baseline as a dot on one scale, the part of the scale
 * that beats the requirement shaded.
 */
export function GrowthRuler({ ev, targetReturn }: { ev: Evaluation; targetReturn: number }) {
  const { min, max, step } = ev.growthScale;
  const x = (v: number) => x0 + ((v - min) / (max - min)) * (x1 - x0);
  const ticks: number[] = [];
  for (let v = Math.ceil(min / step - 1e-9) * step; v <= max + 1e-9; v += step) {
    ticks.push(v);
  }
  const req = ev.required.growth.value;

  // Labels above the scale alternate between two lines when they are close;
  // labels below take the first of three lanes where they do not overlap.
  let lastUp = -1e9;
  let lastUpY = 22;
  const lanesRight = lanes.map(() => -1e9);
  let maxY = 88;
  const marks = marksOf(ev, targetReturn).map((m) => {
    const cx = x(m.value);
    const lx = Math.min(Math.max(cx, x0 + 40), x1 - 40);
    let label: ReactNode;
    if (m.up) {
      const y = lastUp > lx - 120 ? (lastUpY === 22 ? 6 : 22) : 22;
      lastUp = lx;
      lastUpY = y;
      label = (
        <text
          x={lx}
          y={y + 6}
          textAnchor="middle"
          data-walk="ruler-marks"
          style={{ ...labelStyle, fontSize: 12, fill: m.color }}
        >
          {m.label}
        </text>
      );
    } else {
      const w = m.label.length * 6.4 + 10;
      let lane = lanesRight.findIndex((right) => right < lx - w / 2);
      if (lane < 0) {
        lane = lanes.length - 1;
      }
      lanesRight[lane] = lx + w / 2;
      const y = lanes[lane] ?? 112;
      maxY = Math.max(maxY, y + 4);
      label = (
        <text
          x={lx}
          y={y}
          textAnchor="middle"
          data-walk="ruler-marks"
          style={{ ...labelStyle, fontSize: 11.5, fill: m.color }}
        >
          {m.label}
        </text>
      );
    }
    return (
      <MarkDot key={m.key} cx={cx} mark={m}>
        {label}
      </MarkDot>
    );
  });

  return (
    <div className="ruler" data-walk="ruler">
      <svg
        viewBox={`0 0 ${String(width)} ${String(maxY)}`}
        role="img"
        aria-label={`Growth scale: required growth ${pct(req)} against the weighted trend ${pct(ev.baselines.weighted.value)}, today ${pct(ev.baselines.today.value)} and trailing growth ${pct(ev.baselines.trailing.growth.value)}`}
      >
        <line
          x1={x0}
          y1={46}
          x2={x1}
          y2={46}
          stroke="var(--line)"
          strokeWidth={6}
          strokeLinecap="round"
        />
        {req !== null && (
          <line
            data-testid="ruler-beats"
            x1={x(Math.max(req, min))}
            y1={46}
            x2={x1}
            y2={46}
            stroke="color-mix(in srgb, var(--good) 35%, transparent)"
            strokeWidth={6}
            strokeLinecap="round"
          />
        )}
        {ticks.map((v) => (
          <g key={v.toFixed(4)}>
            <line x1={x(v)} y1={54} x2={x(v)} y2={59} stroke="var(--muted)" />
            <text x={x(v)} y={72} textAnchor="middle" style={tickStyle}>
              {Math.round(v * 100)}%
            </text>
          </g>
        ))}
        {marks}
      </svg>
    </div>
  );
}
