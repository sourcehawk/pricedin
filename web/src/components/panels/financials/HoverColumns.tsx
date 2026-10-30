"use client";

import type { FocusEvent, ReactNode } from "react";
import { useTip } from "@/components/tip/TipProvider";
import styles from "./Financials.module.css";
import { cls } from "./metrics";

/**
 * HoverColumns are the hit areas of a chart's quarters, one full-height
 * column each, so the tooltip snaps to the quarter under the pointer.
 * onHover gets the hovered quarter's index, or null when the pointer
 * leaves, so the chart can draw its guide line. With label, each column
 * is also a focusable target named label(i), which shows the same tooltip
 * on keyboard focus.
 */
export function HoverColumns({
  count,
  left,
  width,
  top,
  height,
  tip,
  label,
  onHover,
}: {
  count: number;
  /** The left edge of column i. */
  left: (i: number) => number;
  width: number;
  top: number;
  height: number;
  tip: (i: number) => ReactNode;
  label?: (i: number) => string;
  onHover: (i: number | null) => void;
}) {
  const t = useTip();
  return (
    <>
      {Array.from({ length: count }, (_, i) => (
        <rect
          key={i}
          className={cls("cmpcol", label !== undefined && styles.focusCol)}
          {...(label === undefined
            ? {}
            : {
                tabIndex: 0,
                role: "img",
                "aria-label": label(i),
                onFocus: (e: FocusEvent<SVGRectElement>) => {
                  onHover(i);
                  t.show(e.currentTarget, tip(i));
                },
                onBlur: (e: FocusEvent<SVGRectElement>) => {
                  onHover(null);
                  t.hide(e.currentTarget);
                },
              })}
          x={left(i)}
          y={top}
          width={width}
          height={height}
          fill="transparent"
          onPointerEnter={(e) => {
            onHover(i);
            t.show(e.currentTarget, tip(i), { x: e.clientX, y: e.clientY });
          }}
          onPointerMove={(e) => {
            t.move(e.currentTarget, { x: e.clientX, y: e.clientY });
          }}
          onPointerLeave={(e) => {
            onHover(null);
            t.leave(e.currentTarget);
          }}
        />
      ))}
    </>
  );
}

/** shortLabel is a quarter's label on a chart axis: "Q3 FY26" is "Q3'26". */
export function shortLabel(label: string): string {
  return label.replace(/ FY(\d\d)$/, "'$1");
}
