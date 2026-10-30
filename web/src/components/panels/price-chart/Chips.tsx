"use client";

import { useSyncExternalStore } from "react";
import { colorVar } from "@/components/Swatch";
import { InfoTip } from "@/components/tip/InfoTip";
import { useTipHandlers } from "@/components/tip/TipProvider";
import type { ChartPoint } from "@/engine";
import type { HoverStore } from "./hover";
import { overlays, priceOverlay, type Overlay, type OverlayKey } from "./overlays";
import styles from "./PriceChart.module.css";

function Sample({ o }: { o: Overlay }) {
  return <span className={o.dash === true ? "sw dash" : "sw"} style={colorVar(o.color)} />;
}

function PriceChip({ p }: { p: ChartPoint }) {
  const handlers = useTipHandlers(() => (
    <>
      <b>{priceOverlay.name}</b>
      {priceOverlay.tip}
    </>
  ));
  return (
    <span className={["small", styles.priceChip].join(" ")} tabIndex={0} {...handlers}>
      <Sample o={priceOverlay} />
      <span>{priceOverlay.name}</span>
      <b>{priceOverlay.value?.(p)}</b>
    </span>
  );
}

function Chip({
  o,
  on,
  locked,
  p,
  onToggle,
}: {
  o: Overlay & { key: OverlayKey };
  on: boolean;
  locked: boolean;
  p: ChartPoint;
  onToggle: (key: OverlayKey) => void;
}) {
  const handlers = useTipHandlers(() => (
    <>
      <b>{o.name}</b>
      {o.tip}
      <div className="tm">
        {locked ? "Every line stays on during the walkthrough." : "Click to show or hide."}
      </div>
    </>
  ));
  return (
    <button
      type="button"
      aria-pressed={on}
      aria-disabled={locked}
      {...handlers}
      onClick={(e) => {
        handlers.onClick(e);
        if (!locked) {
          onToggle(o.key);
        }
      }}
    >
      <Sample o={o} />
      {o.name}
      {on && o.value !== undefined && <b>{o.value(p)}</b>}
    </button>
  );
}

/**
 * Chips are the legend of the price chart and its toggles. Each shows its
 * line's value on the hovered day, or on the last day when none is hovered.
 * While locked, a click does not call onToggle.
 */
export function Chips({
  points,
  hover,
  shown,
  locked,
  onToggle,
}: {
  points: readonly ChartPoint[];
  hover: HoverStore;
  shown: ReadonlySet<OverlayKey>;
  locked: boolean;
  onToggle: (key: OverlayKey) => void;
}) {
  const i = useSyncExternalStore(hover.subscribe, hover.get, hover.get);
  const p = (i === null ? undefined : points[i]) ?? points.at(-1);
  if (p === undefined) {
    return null;
  }
  return (
    <div className="chips" role="group" aria-label="Lines on the price chart">
      <PriceChip p={p} />
      {overlays.map((o) => (
        <Chip key={o.key} o={o} on={shown.has(o.key)} locked={locked} p={p} onToggle={onToggle} />
      ))}
      <InfoTip label="About overlays">
        <b>Lines on the chart</b>
        Only the price uses the left axis. Each other line is stretched to the chart&apos;s height
        so you can compare shapes, so read their values from the chips or by hovering. Price change,
        12M: how much the share price moved over the past year. EPS growth, 12M: how much TTM EPS
        grew over the same year. These two share one scale, and the faint line is their 0%. When
        price change runs above EPS growth, the P/E went up, as long as TTM EPS stayed positive.
        Comparing 12-month changes instead of levels avoids a false link: two things that both trend
        up always look correlated.
      </InfoTip>
    </div>
  );
}
