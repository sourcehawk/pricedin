"use client";

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import type { Axis } from "@/engine";
import { Sourced } from "@/components/Sourced";
import { useTip } from "@/components/tip/TipProvider";
import type { Provenance } from "@/state/StockProvider";
import styles from "./smallChart.module.css";

// The parts that the small charts (the earnings trajectory and how
// demanding over time) share: the hover columns with their guide line,
// the y axis and the rows of the tooltip.

/** swatch sets the color of a legend key or a line sample (the --c of .keys and .lg). */
export function swatch(color: string): CSSProperties {
  return { "--c": color } as CSSProperties;
}

/** TipRow is one label and value row of a chart tooltip. */
export function TipRow({ label, value }: { label: ReactNode; value: ReactNode }) {
  return (
    <div className="row">
      <span>{label}</span>
      <span>{value}</span>
    </div>
  );
}

/** YAxis draws the grid line and the label of each tick of axis between x0 and x1. */
export function YAxis({
  axis,
  y,
  x0,
  x1,
  format,
}: {
  axis: Axis;
  y: (v: number) => number;
  x0: number;
  x1: number;
  format: (v: number) => string;
}) {
  const ticks: number[] = [];
  if (axis.step > 0) {
    for (let k = Math.ceil(axis.min / axis.step - 1e-9); k * axis.step <= axis.max + 1e-9; k++) {
      ticks.push(k * axis.step);
    }
  }
  return (
    <g aria-hidden="true">
      {ticks.map((v) => (
        <g key={v}>
          <line x1={x0} y1={y(v)} x2={x1} y2={y(v)} stroke="var(--line)" strokeWidth={1} />
          <text x={x0 - 6} y={y(v) + 3.5} textAnchor="end">
            {format(v)}
          </text>
        </g>
      ))}
    </g>
  );
}

/** ColumnsLayout places the hover columns: one per x position, centered on it. */
export interface ColumnsLayout {
  count: number;
  x: (i: number) => number;
  /** The width of one column. */
  width: number;
  y0: number;
  y1: number;
  /** The column that focus shows first; the last one when absent. */
  start?: number;
}

const keySteps: Record<string, (i: number, count: number) => number> = {
  ArrowLeft: (i) => i - 1,
  ArrowRight: (i) => i + 1,
  Home: () => 0,
  End: (_, count) => count - 1,
};

/**
 * useColumns makes a small chart's columns hoverable: the tooltip snaps to
 * the column under the pointer and a vertical guide line marks it. Spread
 * chartProps on the svg, which makes it focusable: focus shows the start
 * column, and the arrow keys, Home and End move between columns. Render
 * layer inside the svg, above the marks, and live outside the svg: it
 * announces the column that the keys reach. Pass as data the value that
 * tipFor reads, and a new value when it changes: an open tooltip then shows
 * the new values.
 */
export function useColumns(layout: ColumnsLayout, tipFor: (i: number) => ReactNode, data: unknown) {
  const tip = useTip();
  const [active, setActive] = useState<number | null>(null);
  const [keyboard, setKeyboard] = useState(false);
  const rects = useRef<(SVGRectElement | null)[]>([]);
  const pointerAt = useRef<{ x: number; y: number } | undefined>(undefined);
  const latestTip = useRef(tipFor);
  const owned = useRef<SVGRectElement | null>(null);
  const { count, x, width, y0, y1 } = layout;
  const start = layout.start ?? count - 1;

  // Fewer columns, such as after a shorter holding period, can remove the active one.
  if (active !== null && active >= count) {
    setActive(null);
  }

  useLayoutEffect(() => {
    latestTip.current = tipFor;
  });
  // The tooltip holds the content it was shown with, so new levers would
  // leave stale values in it, or keep it open for a column that is gone.
  useEffect(() => {
    const el = active === null ? null : (rects.current[active] ?? null);
    if (active !== null && el !== null && tip.owner === el) {
      tip.show(el, latestTip.current(active), pointerAt.current);
    } else if (owned.current !== null && tip.owner === owned.current && el !== owned.current) {
      tip.hide(owned.current);
    }
  }, [data, active, tip]);
  const hideTip = tip.hide;
  useEffect(
    () => () => {
      if (owned.current !== null) {
        hideTip(owned.current);
      }
    },
    [hideTip],
  );

  const show = (i: number, at?: { x: number; y: number }) => {
    const el = rects.current[i] ?? null;
    pointerAt.current = at;
    owned.current = el;
    setActive(i);
    if (el !== null) {
      tip.show(el, tipFor(i), at);
    }
  };
  const hide = () => {
    setActive(null);
    if (tip.owner !== null && rects.current.includes(tip.owner as SVGRectElement)) {
      tip.hide(tip.owner);
    }
  };
  const pointer = (i: number) => (e: PointerEvent<SVGRectElement>) => {
    setKeyboard(false);
    if (e.pointerType !== "touch") {
      show(i, { x: e.clientX, y: e.clientY });
    } else if (e.type === "pointerdown") {
      if (active === i && tip.owner === e.currentTarget) {
        hide();
      } else {
        show(i, { x: e.clientX, y: e.clientY });
      }
    }
  };
  const showKey = (i: number) => {
    setKeyboard(true);
    show(i);
  };

  const chartProps = {
    className: styles.plot,
    tabIndex: 0,
    // A finger that lifts also leaves the chart; the tapped column stays.
    onPointerLeave: (e: PointerEvent<SVGSVGElement>) => {
      if (e.pointerType !== "touch") {
        hide();
      }
    },
    onFocus: (e: React.FocusEvent<SVGSVGElement>) => {
      if (e.target === e.currentTarget && count > 0) {
        showKey(active ?? start);
      }
    },
    onBlur: () => {
      setKeyboard(false);
      hide();
    },
    onKeyDown: (e: KeyboardEvent<SVGSVGElement>) => {
      const step = keySteps[e.key];
      if (step === undefined || count === 0) {
        return;
      }
      e.preventDefault();
      showKey(Math.min(count - 1, Math.max(0, step(active ?? start, count))));
    },
  };

  const live = (
    <div role="status" className={styles.live}>
      {keyboard && active !== null ? tipFor(active) : null}
    </div>
  );

  const gx = active === null ? 0 : x(active);
  const layer = (
    <g>
      {Array.from({ length: count }, (_, i) => (
        <rect
          key={i}
          ref={(el) => {
            rects.current[i] = el;
          }}
          className="vcol"
          data-x={x(i).toFixed(1)}
          x={x(i) - width / 2}
          y={y0}
          width={width}
          height={y1 - y0}
          fill="transparent"
          onPointerEnter={pointer(i)}
          onPointerDown={pointer(i)}
          onPointerMove={(e) => {
            pointerAt.current = { x: e.clientX, y: e.clientY };
            tip.move(e.currentTarget, pointerAt.current);
          }}
        />
      ))}
      <line
        className="vx"
        x1={gx.toFixed(1)}
        x2={gx.toFixed(1)}
        y1={y0}
        y2={y1}
        stroke="var(--ink)"
        strokeWidth={1}
        opacity={0.4}
        visibility={active === null ? "hidden" : "visible"}
        pointerEvents="none"
      />
    </g>
  );
  return { active, chartProps, layer, live };
}

/** linePath joins the points into an SVG path; null points and points without a finite y are left out. */
export function linePath(points: readonly (readonly [number, number] | null)[]): string {
  return points
    .filter((p): p is readonly [number, number] => p !== null && Number.isFinite(p[1]))
    .map((p, i) => `${i === 0 ? "M" : "L"}${p[0].toFixed(1)},${p[1].toFixed(1)}`)
    .join(" ");
}

/** endLabels spaces two right-edge labels at least 13 apart so they never overlap. */
export function endLabels(a: number, b: number): [number, number] {
  let ya = a + 3.5;
  let yb = b + 3.5;
  if (Math.abs(ya - yb) < 13) {
    if (ya > yb) {
      ya = yb + 13;
    } else {
      yb = ya + 13;
    }
  }
  return [ya, yb];
}

/** FromLevers shows a starting-point value with its provenance: the source of the stock's value, or a mark that the user typed it. */
export function FromLevers({
  provenance,
  children,
}: {
  provenance: Provenance;
  children: ReactNode;
}) {
  return (
    <Sourced
      value={provenance?.kind === "stock" ? provenance.sourced : null}
      user={provenance?.kind === "user"}
    >
      {children}
    </Sourced>
  );
}
