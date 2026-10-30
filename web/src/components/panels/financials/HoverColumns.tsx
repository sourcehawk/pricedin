"use client";

import { useRef, type FocusEvent, type ReactNode } from "react";
import { useTip } from "@/components/tip/TipProvider";
import styles from "./Financials.module.css";
import { cls } from "./metrics";

/**
 * HoverColumns are the hit areas of a chart's quarters, one full-height
 * column each, so the tooltip snaps to the quarter under the pointer.
 * onHover gets the hovered quarter's index, or null when the pointer
 * leaves, so the chart can draw its guide line. With label, each column
 * is also a focusable target named label(i), which shows the same tooltip
 * on keyboard focus. A tap shows the tooltip and the next tap hides it.
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
  const pointer = useRef("mouse");
  // A tap focuses a focusable column before its click, and focus shows the
  // tip, so the toggle reads whether the tip showed before the tap began.
  const openAtTap = useRef(false);
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
          onPointerDown={(e) => {
            pointer.current = e.pointerType;
            openAtTap.current = t.owner === e.currentTarget;
          }}
          onPointerEnter={(e) => {
            if (e.pointerType !== "touch") {
              onHover(i);
              t.show(e.currentTarget, tip(i), { x: e.clientX, y: e.clientY });
            }
          }}
          onPointerMove={(e) => {
            if (e.pointerType !== "touch") {
              t.move(e.currentTarget, { x: e.clientX, y: e.clientY });
            }
          }}
          onPointerLeave={(e) => {
            if (e.pointerType !== "touch") {
              onHover(null);
              t.leave(e.currentTarget);
            }
          }}
          onClick={(e) => {
            if (pointer.current !== "touch") {
              return;
            }
            if (openAtTap.current) {
              onHover(null);
              t.hide(e.currentTarget);
            } else {
              onHover(i);
              t.show(e.currentTarget, tip(i));
            }
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
