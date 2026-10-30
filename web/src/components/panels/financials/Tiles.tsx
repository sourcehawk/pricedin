"use client";

import type { ReactNode } from "react";
import type { SourcedNumber } from "@/api/types";
import { pct, pp, signedPct } from "@/components/format";
import { EstimateTag, Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import type { FinancialsView, MaybeNumber, Metric, Tile as EngineTile, Tone } from "@/engine";
import styles from "./Financials.module.css";
import {
  cls,
  marginTip,
  metricInfo,
  metricValue,
  missingText,
  showsAsZero,
  toneClass,
} from "./metrics";

function Tile({
  walk,
  label,
  about,
  tip,
  value,
  sourced,
  change,
}: {
  /** The tile's data-walk key. */
  walk: string;
  label: string;
  /** The name in the label of the info button. */
  about: string;
  tip: ReactNode;
  value: string;
  sourced: SourcedNumber | null;
  /** The change line; null when the change is missing. */
  change: { text: string; tone: Tone | null };
}) {
  return (
    <div className="tile" data-walk={walk}>
      <span className="eyebrow">
        <span>{label}</span>
        <InfoTip label={`About ${about}`}>
          <b>{about}</b>
          {tip}
        </InfoTip>
      </span>
      <b>
        <Sourced value={sourced}>{value}</Sourced>
        <EstimateTag source={sourced?.source} />
      </b>
      <span className={`small ${toneClass(change.tone)}`} data-walk="tile-change">
        {change.text}
      </span>
    </div>
  );
}

function changeLine(
  c: MaybeNumber,
  tone: { value: Tone | null },
  text: (v: number) => string,
): { text: string; tone: Tone | null } {
  if (c.value === null) {
    return { text: `n/a: ${missingText(c)}`, tone: null };
  }
  return { text: text(c.value), tone: tone.value };
}

/**
 * Tiles compare the latest quarter with the same quarter a year earlier.
 * latest holds each metric's source in the latest quarter.
 */
export function Tiles({
  view,
  latest,
  margin,
}: {
  view: FinancialsView;
  latest: Record<Metric, SourcedNumber | null>;
  margin: SourcedNumber | null;
}) {
  const t = view.tiles.value;
  if (t === null) {
    return (
      <p className="note">
        The tiles compare the latest quarter with the same quarter a year earlier, so they need five
        quarters of filings.
      </p>
    );
  }
  const revChange = t.revenue.change.value;
  const tile = (
    m: Metric,
    label: string,
    more: (c: number, e: EngineTile) => string = () => "",
  ) => {
    const e = t[m];
    const digits = m === "dilutedShares" ? 1 : 0;
    const c = e.change.value;
    const tone = { value: c === null || showsAsZero(c, digits) ? null : e.tone.value };
    return (
      <Tile
        walk={`tile-${m}`}
        label={label}
        about={metricInfo[m].name}
        tip={metricInfo[m].tip}
        value={metricValue(m, e.latest.value, 2)}
        sourced={latest[m]}
        change={changeLine(e.change, tone, (c) => `${signedPct(c, digits)} YoY${more(c, e)}`)}
      />
    );
  };
  const mt = t.operatingMargin;
  return (
    <div className={cls("tiles", styles.tiles)}>
      {tile("revenue", `Revenue ${t.label}`)}
      {tile("operatingCosts", "Operating costs", (c, e) => {
        let s = ` · revenue ${signedPct(revChange, 0)}`;
        if (e.tone.value === "warn") {
          if (revChange === null || revChange >= 0) {
            s += " (costs rose faster than sales)";
          } else {
            s += c < 0 ? " (costs fell less than sales)" : " (costs rose while sales fell)";
          }
        }
        return s;
      })}
      {tile("operatingIncome", "Operating income")}
      <Tile
        walk="tile-margin"
        label="Operating margin"
        about="Operating margin"
        tip={marginTip}
        value={pct(mt.latest.value)}
        sourced={margin}
        change={changeLine(mt.change, mt.tone, (c) => pp(c))}
      />
      {tile("eps", "Diluted EPS")}
      {tile("freeCashFlow", "Free cash flow")}
      {tile("dilutedShares", "Diluted shares", (c) =>
        showsAsZero(c, 1) ? "" : c > 0 ? " (dilution)" : " (buybacks)",
      )}
    </div>
  );
}
