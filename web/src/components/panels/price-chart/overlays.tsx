import type { ReactNode } from "react";
import { signedPct, times, usd } from "@/components/format";
import type { ChartPoint } from "@/engine";
import { Swatch } from "./Swatch";

/** OverlayKey names a line or marker set that a chip switches on and off. */
export type OverlayKey = "eps" | "pe" | "r12" | "g12" | "sh" | "bands";

/** Overlay is one chip of the price chart: its legend sample, name, value and explanation. */
export interface Overlay {
  key: OverlayKey | "price";
  name: string;
  /** A color token for the legend sample. */
  color: string;
  dash?: boolean;
  /** The value of the day; overlays without a value per day have none. */
  value?: (p: ChartPoint) => string;
  tip: ReactNode;
}

export const priceOverlay: Overlay = {
  key: "price",
  name: "Price",
  color: "var(--accent)",
  value: (p) => usd(p.close),
  tip: "The share price at each day’s close. It is the only line that uses the left axis.",
};

/** overlays are the chips that switch, in the order of the chip row. */
export const overlays: readonly (Overlay & { key: OverlayKey })[] = [
  {
    key: "eps",
    name: "TTM EPS",
    color: "var(--ink)",
    value: (p) => usd(p.ttmEps),
    tip: "Earnings per share over the last 12 months: the last four quarterly reports added up. It steps when a new report comes out. If the price rises with it, the price follows earnings.",
  },
  {
    key: "pe",
    name: "P/E",
    color: "var(--warn)",
    value: (p) => times(p.pe.value),
    tip: "Price divided by TTM EPS: how many dollars investors pay for one dollar of yearly earnings. A rising line means the stock got more expensive compared with its earnings.",
  },
  {
    key: "r12",
    name: "Price change, 12M",
    color: "var(--accent)",
    dash: true,
    value: (p) => signedPct(p.priceChange12m.value, 0),
    tip: "How much the share price changed compared with the same day one year earlier.",
  },
  {
    key: "g12",
    name: "EPS growth, 12M",
    color: "var(--ink)",
    dash: true,
    value: (p) => signedPct(p.epsGrowth12m.value, 0),
    tip: "How much TTM EPS grew compared with one year earlier. It shares a scale with Price change, 12M. When the price change runs above it, the P/E went up, as long as TTM EPS stayed positive.",
  },
  {
    key: "sh",
    name: "Share changes",
    color: "var(--bad)",
    tip: (
      <>
        Markers on quarters where the diluted share count changed by 2% or more.{" "}
        <Swatch color="var(--bad)" /> The count rose (dilution), so each share owns a smaller part
        of the company. <Swatch color="var(--good)" /> The count fell, usually through buybacks.
        Hover a marker for details.
      </>
    ),
  },
  {
    key: "bands",
    name: "P/E bands",
    color: "var(--muted)",
    dash: true,
    tip: "Each dotted line is the share price at one fixed P/E (labeled on the right): TTM EPS times that number, so it steps up when earnings rise. Where the price sits among the lines is its P/E. Climbing to a higher line means investors pay more per dollar of earnings; staying with one line means the price only followed earnings. Hover a line for its value.",
  },
];

export const allOverlays: readonly OverlayKey[] = overlays.map((o) => o.key);

/** defaultOverlays are on until the user switches them. */
export const defaultOverlays: readonly OverlayKey[] = ["eps", "pe", "sh"];

/** overlaysView is the view key of the chips; its value lists the overlays that are on. */
export const overlaysView = "price.overlays";

/** overlaySet keeps the known overlay keys of a stored view value. */
export function overlaySet(keys: readonly string[]): ReadonlySet<OverlayKey> {
  return new Set(allOverlays.filter((k) => keys.includes(k)));
}
