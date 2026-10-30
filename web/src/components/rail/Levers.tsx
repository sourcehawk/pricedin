"use client";

import { useId, type ReactNode } from "react";
import type { BaselineMode, ExitPeMode } from "@/engine";
import { pct, usd, years } from "@/components/format";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStock } from "@/state/StockProvider";
import { NumberField } from "./NumberField";
import { baselineNote, exitPeNote, trajectoryParts } from "./notes";

/** Seg is the prototype's segmented switch: one pressed button out of a few. */
export function Seg<T extends string | number>({
  label,
  options,
  value,
  onChange,
  className = "seg",
}: {
  label: string;
  options: readonly { value: T; label: ReactNode }[];
  value: T | null;
  onChange: (v: T) => void;
  className?: string;
}) {
  return (
    <div className={className} role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => {
            onChange(o.value);
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

const tenths = (v: number): string => (Math.round(v * 10) / 10).toFixed(1);
const plain = (v: number): string => String(Math.round(v * 1000) / 10);

/** TargetReturnLever sets the target return: a percent field and a slider. */
export function TargetReturnLever() {
  const { state, dispatch } = useStock();
  const id = useId();
  const r = state.targetReturn * 100;
  const set = (v: number) => {
    dispatch({ type: "setTargetReturn", value: v / 100 });
  };
  return (
    <div className="lever" data-lever="targetReturn">
      <div className="lever-head">
        <span className="lh">
          <label htmlFor={id}>Target return</label>
          <InfoTip label="About the target return">
            Your hurdle rate: the yearly return that makes the investment worth it to you. The
            S&amp;P 500 returned about 10% a year on average over the long run, so 15% asks the
            stock to clearly beat the index.
          </InfoTip>
        </span>
        <span className="numfield">
          <NumberField
            aria-label="Target return, percent a year"
            min={0}
            max={50}
            step={0.5}
            inputMode="decimal"
            text={tenths(r)}
            onValue={set}
          />
          <span>%</span>
        </span>
      </div>
      <input
        id={id}
        type="range"
        min={0}
        max={25}
        step={0.5}
        value={Math.min(25, r)}
        onChange={(e) => {
          set(Number(e.target.value));
        }}
      />
      <div className="tickrow" aria-hidden="true">
        <span style={{ left: 0 }}>0%</span>
        <span className="mid" style={{ left: "40%" }}>
          10% S&amp;P avg
        </span>
        <span className="mid" style={{ left: "60%" }}>
          15%
        </span>
        <span className="end" style={{ left: "100%" }}>
          25%
        </span>
      </div>
    </div>
  );
}

/** HoldingPeriodLever sets the holding period in whole years. */
export function HoldingPeriodLever() {
  const { state, dispatch } = useStock();
  const id = useId();
  const n = state.holdingYears;
  const set = (v: number) => {
    dispatch({ type: "setHoldingYears", value: v });
  };
  return (
    <div className="lever" data-lever="holdingYears">
      <div className="lever-head">
        <span className="lh">
          <label htmlFor={id}>Holding period</label>
          <InfoTip label="About the holding period">
            Years until you assume you sell at the exit P/E. Over a longer period, a change in the
            multiple matters less per year, and earnings growth matters more.
          </InfoTip>
        </span>
        <span className="numfield">
          <NumberField
            className="w-sm"
            aria-label="Holding period, years"
            min={1}
            max={10}
            step={1}
            inputMode="numeric"
            text={String(n)}
            onValue={set}
          />
          <span>&nbsp;{n === 1 ? "year" : "years"}</span>
        </span>
      </div>
      <input
        id={id}
        type="range"
        min={1}
        max={10}
        step={1}
        value={n}
        onChange={(e) => {
          set(Number(e.target.value));
        }}
      />
      <Seg
        label="Holding period"
        options={[1, 3, 5, 10].map((y) => ({ value: y, label: `${String(y)}Y` }))}
        value={n}
        onChange={set}
      />
    </div>
  );
}

const exitModes: readonly { value: ExitPeMode; label: string }[] = [
  { value: "estimated", label: "Estimated" },
  { value: "today", label: "Today" },
  { value: "trailing", label: "Trailing" },
  { value: "custom", label: "Custom" },
];

const sliderMin = 8;

/** ExitPeLever sets the exit P/E: its mode, a custom value and the typical P/E's look-back. */
export function ExitPeLever() {
  const { state, dispatch, evaluation: ev } = useStock();
  const id = useId();
  const monthsId = useId();
  const inUse = ev?.exitPe.inUse.value?.pe ?? null;
  const shown = state.exitPe.mode === "custom" ? state.exitPe.custom : inUse;
  const max = ev?.exitPe.sliderMax ?? 45;
  const now = ev?.peNow.value ?? null;
  const f = now === null ? 0.5 : Math.min(1, Math.max(0, (now - sliderMin) / (max - sliderMin)));
  const typical = ev?.exitPe.typical.pe.value ?? null;
  const setCustom = (v: number) => {
    dispatch({ type: "setExitPe", value: v });
  };
  return (
    <div className="lever" data-lever="exitPe">
      <div className="lever-head">
        <span className="lh">
          <label htmlFor={id}>Exit P/E multiple</label>
          <InfoTip label="About the exit P/E">
            The P/E is the share price divided by yearly earnings: how much investors pay for one
            dollar of profit. The exit P/E is the P/E you expect when you sell, and it matters a
            lot. Selling at 20× instead of 25× gives a 20% lower price for the same earnings.
            Estimated (default): lowers today&apos;s P/E as growth slows, because investors pay less
            for slower growth. When growth is already slow or negative and today&apos;s P/E is above
            the stock&apos;s typical level, it uses that level instead. The typical P/E line under
            the slider shows that level, and you can change its months there without picking
            Trailing. Today: keeps today&apos;s P/E. Trailing: the median P/E over the months you choose (36 by default), the
            stock&apos;s typical level. Custom: type or drag your own number.
          </InfoTip>
        </span>
        <span className="numfield">
          <NumberField
            aria-label="Exit P/E multiple, type a number"
            min={0.5}
            max={1000}
            step={0.5}
            inputMode="decimal"
            text={shown === null ? "" : shown.toFixed(1)}
            onValue={(v) => {
              if (v > 0) {
                setCustom(v);
              }
            }}
          />
          <span>×</span>
        </span>
      </div>
      <Seg
        label="Exit P/E"
        options={exitModes}
        value={state.exitPe.mode}
        onChange={(mode) => {
          dispatch({ type: "setExitPeMode", mode, current: inUse });
        }}
      />
      <input
        id={id}
        type="range"
        min={sliderMin}
        max={max}
        step={0.5}
        value={shown ?? 20}
        onChange={(e) => {
          setCustom(Number(e.target.value));
        }}
      />
      <div className="tickrow" aria-hidden="true">
        <span style={{ left: 0 }} hidden={now !== null && f < 0.25}>
          {sliderMin}×
        </span>
        <span className="mid" style={{ left: `${String(f * 100)}%` }}>
          {now === null ? "" : `now ${now.toFixed(1)}×`}
        </span>
        <span className="end" style={{ left: "100%" }} hidden={now !== null && f > 0.72}>
          {max}×
        </span>
      </div>
      <div className="trlrow" data-lever="typicalPeMonths">
        <span className="muted">Typical P/E</span>
        <b className="refv">
          {typical === null ? "n/a" : `${(Math.round(typical * 2) / 2).toFixed(1)}×`}
        </b>
        <label htmlFor={monthsId} className="muted">
          · last
        </label>
        <NumberField
          id={monthsId}
          min={6}
          max={36}
          step={3}
          inputMode="numeric"
          text={String(state.exitPe.trailingMonths)}
          onValue={(v) => {
            if (v > 0) {
              dispatch({ type: "setExitPeMonths", value: v });
            }
          }}
          onBlur={() => {
            if (ev !== null) {
              dispatch({ type: "setExitPeMonths", value: ev.exitPe.typical.months });
            }
          }}
        />
        <span className="muted small">months</span>
      </div>
      {ev !== null && <p className="lever-note">{exitPeNote(ev)}</p>}
    </div>
  );
}

const baselineModes: readonly { value: BaselineMode; label: string }[] = [
  { value: "weighted", label: "Weighted" },
  { value: "today", label: "Today" },
  { value: "trailing", label: "Trailing" },
  { value: "custom", label: "Custom" },
];

/** BaselineLever sets the growth baseline, with the custom rates and the trajectory fill. */
export function BaselineLever() {
  const { state, dispatch, evaluation: ev, levers } = useStock();
  const monthsId = useId();
  const b = state.baseline;
  const growth = ev?.baselines.growth.value ?? null;
  const n = state.holdingYears;
  const shownRates =
    ev?.yearByYear.value?.rates ??
    Array.from({ length: n }, (_, i) => b.rates[i] ?? b.rates.at(-1) ?? b.rate);
  const trajectory = ev?.trajectory.value ?? null;
  const follow = () => {
    if (trajectory !== null) {
      dispatch({ type: "followTrajectory", rates: trajectory.rates });
    }
  };
  const parts = ev === null ? null : trajectoryParts(ev);
  const yby = ev?.yearByYear.value ?? null;
  return (
    <div className="lever" data-lever="baseline">
      <div className="lever-head">
        <span className="lh">
          <span className="label">Growth baseline</span>
          <InfoTip label="About the growth baseline">
            The growth you compare against what the price requires. Weighted (default):
            year-over-year EPS growth of the last 8 quarters, with the latest quarters counting
            most. Today: the latest quarter only, the freshest reading but the noisiest. Trailing:
            average yearly growth of TTM EPS over a number of months you choose. Custom: your own
            number, or one per year.
          </InfoTip>
        </span>
        <span className="numfield">
          <NumberField
            aria-label="Growth baseline, percent a year; typing sets a custom rate"
            min={-50}
            max={200}
            step={0.5}
            inputMode="decimal"
            text={growth === null ? "" : tenths(growth * 100)}
            onValue={(v) => {
              dispatch({ type: "setGrowth", value: v / 100 });
            }}
          />
          <span>%</span>
        </span>
      </div>
      <Seg
        label="Growth baseline"
        options={baselineModes}
        value={b.mode}
        onChange={(mode) => {
          dispatch({ type: "setBaselineMode", mode });
        }}
      />
      <div className="trlrow" data-lever="trailingMonths">
        <span className="muted">Trailing growth</span>
        <b className="refv">{pct(ev?.baselines.trailing.growth.value)}</b>
        <label htmlFor={monthsId} className="muted">
          · last
        </label>
        <NumberField
          id={monthsId}
          min={6}
          max={120}
          step={3}
          inputMode="numeric"
          text={String(b.trailingMonths)}
          onValue={(v) => {
            if (v > 0) {
              dispatch({ type: "setBaselineMonths", value: v });
            }
          }}
          onBlur={() => {
            if (ev !== null) {
              dispatch({ type: "setBaselineMonths", value: ev.baselines.trailing.months });
            }
          }}
        />
        <span className="muted small">months</span>
      </div>
      {b.mode === "custom" && (
        <div className="lever" style={{ marginTop: 4 }}>
          <Seg
            label="Custom growth"
            options={[
              { value: "flat", label: "Same each year" },
              { value: "yearly", label: "Year by year" },
            ]}
            value={b.yearly ? "yearly" : "flat"}
            onChange={(v) => {
              dispatch({ type: "setYearly", yearly: v === "yearly" });
            }}
          />
          {!b.yearly ? (
            <div className="lever" data-lever="customGrowth">
              <input
                type="range"
                min={-10}
                max={40}
                step={0.5}
                aria-label="Custom EPS growth"
                value={Math.max(-10, Math.min(40, b.rate * 100))}
                onChange={(e) => {
                  dispatch({ type: "setCustomRate", value: Number(e.target.value) / 100 });
                }}
              />
              <div className="ticks" aria-hidden="true">
                <span>-10%</span>
                <span>15%</span>
                <span>40%</span>
              </div>
            </div>
          ) : (
            <div data-lever="yearRates">
              <div className="yeargrid">
                {shownRates.map((r, i) => (
                  <label key={i}>
                    Y{i + 1}
                    <NumberField
                      step={0.5}
                      aria-label={`EPS growth in year ${String(i + 1)}, percent`}
                      text={plain(r)}
                      onValue={(v) => {
                        dispatch({
                          type: "setYearRate",
                          index: i,
                          value: v / 100,
                          current: shownRates,
                        });
                      }}
                    />
                  </label>
                ))}
              </div>
              {yby !== null && levers !== null && levers.ttmEps > 0 && (
                <p className="small year-sum" style={{ margin: "8px 0 0" }}>
                  Average <b>{pct(yby.compoundAverage)}</b> a year. EPS goes from{" "}
                  {usd(levers.ttmEps)} to <b>{usd(yby.endEps.value)}</b> in {years(n)}.
                  {yby.showSimpleAverage && (
                    <span className="muted">
                      {" "}
                      The simple average of your rates is {pct(yby.simpleAverage)}. The compound
                      average is what counts, because each year builds on the one before.
                    </span>
                  )}
                </p>
              )}
              {b.ratesSource !== null && (
                <p className="lever-note" style={{ marginTop: 6 }}>
                  {b.ratesSource === "trajectory"
                    ? "Filled from the weighted trend’s trajectory. Change any year to make it your own."
                    : "Your own rates."}
                </p>
              )}
              <button
                type="button"
                className="btn-ghost"
                style={{ marginTop: 8 }}
                onClick={follow}
                disabled={trajectory === null}
              >
                Refill from weighted trend
              </button>
            </div>
          )}
        </div>
      )}
      {ev !== null && levers !== null && <p className="lever-note">{baselineNote(ev, levers)}</p>}
      {b.mode === "weighted" && parts !== null && (
        <div className="trajbox">
          <p className="small" style={{ margin: 0 }}>
            {parts.lead}
            <b>{parts.rates}</b>
            {parts.tail}
          </p>
          <button type="button" className="btn-ghost" onClick={follow}>
            Follow this trend year by year
          </button>
        </div>
      )}
    </div>
  );
}
