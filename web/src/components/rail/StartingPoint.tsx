"use client";

import { useId } from "react";
import { usd } from "@/components/format";
import { Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStock } from "@/state/StockProvider";
import type { StartingPointField } from "@/state/levers";
import { NumberField } from "./NumberField";

const short = (v: number | null | undefined): string =>
  v === null || v === undefined ? "" : String(Number(v.toFixed(4)));

/**
 * StartingPoint shows the share price, TTM EPS and yearly dividend that the
 * levers start from. They come from the stock and stay editable; an empty
 * field goes back to the stock's value.
 */
export function StartingPoint() {
  const { state, dispatch, profile, levers, provenance } = useStock();
  const p = profile.data;
  const ids = { price: useId(), ttmEps: useId(), dividend: useId() };
  const value = (f: StartingPointField) =>
    state[f] ??
    (f === "price"
      ? p?.latestClose?.value
      : f === "ttmEps"
        ? p?.ttmEps?.value
        : (p?.ttmDividend.value ?? null));
  const field = (f: StartingPointField, extra: { min?: number }) => (
    <NumberField
      id={ids[f]}
      step={0.01}
      inputMode="decimal"
      {...extra}
      text={short(levers?.[f] ?? value(f))}
      onValue={(v) => {
        dispatch({ type: "setStartingPoint", field: f, value: v });
      }}
      onEmpty={() => {
        dispatch({ type: "setStartingPoint", field: f, value: null });
      }}
    />
  );
  const edited =
    p !== undefined &&
    (["price", "ttmEps", "dividend"] as const).some((f) => provenance[f]?.kind === "user");
  const sym = state.ticker ?? "";
  return (
    <div>
      <div className="eyebrow">Starting point</div>
      {p !== undefined && (
        <p className="small muted" style={{ margin: "4px 0 0" }}>
          {edited ? (
            <>
              Edited by you. {sym} closed at{" "}
              <Sourced value={p.latestClose}>{usd(p.latestClose?.value)}</Sourced> with{" "}
              <Sourced value={p.ttmEps}>{usd(p.ttmEps?.value)}</Sourced> TTM EPS.
            </>
          ) : (
            <>
              Filled from {sym}: the close of{" "}
              <Sourced value={p.latestClose}>{p.latestClose?.asOf ?? "n/a"}</Sourced> and the last
              four quarters of diluted EPS (
              <Sourced value={p.ttmEps}>{usd(p.ttmEps?.value)}</Sourced>
              ). Type over to override.
            </>
          )}
        </p>
      )}
      <div className="inputs" style={{ marginTop: 8 }}>
        <div className="field" data-lever="price">
          <label htmlFor={ids.price}>Share price ($)</label>
          {field("price", { min: 0.01 })}
        </div>
        <div className="field" data-lever="ttmEps">
          <label htmlFor={ids.ttmEps}>TTM EPS ($)</label>
          {field("ttmEps", {})}
        </div>
        <div className="field" style={{ gridColumn: "1/-1" }} data-lever="dividend">
          <label htmlFor={ids.dividend}>
            Dividend per share, yearly ($)
            <InfoTip label="About dividends">
              Counted as cash you receive each year, not reinvested. A dividend lowers the EPS
              growth you need. The page fills it with the recurring dividends of the last 12 months;
              set it to 0 to look at the price return only.
            </InfoTip>
          </label>
          {field("dividend", { min: 0 })}
        </div>
      </div>
    </div>
  );
}
