"use client";

import { useMemo, useState, type ReactNode } from "react";
import { signedPct, times, usd } from "@/components/format";
import { filingLink, Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import { useWalkthrough } from "@/components/walkthrough/WalkthroughProvider";
import type { Chart, ChartRange } from "@/engine";
import { useEvaluation, useView } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { Chips } from "./Chips";
import { dayLabel, multipleLabel, spanLabel } from "./geometry";
import { createHoverStore } from "./hover";
import {
  allOverlays,
  defaultOverlays,
  overlaySet,
  overlaysView,
  type OverlayKey,
} from "./overlays";
import { PricePlot, type Marker } from "./PricePlot";
import styles from "./PriceChart.module.css";
import { bandsStep, priceSteps } from "./steps";

const ranges: readonly ChartRange[] = ["6M", "1Y", "3Y"];
const walkthroughId = "price-chart";
const everyOverlay: ReadonlySet<OverlayKey> = new Set(allOverlays);

const vsWords = { at: "right at", above: "above", below: "below" } as const;

function toggled(s: ReadonlySet<OverlayKey>, k: OverlayKey): Set<OverlayKey> {
  const next = new Set(s);
  if (next.has(k)) {
    next.delete(k);
  } else {
    next.add(k);
  }
  return next;
}

function BandCaption({ chart, lifted }: { chart: Chart; lifted: number | null }) {
  const bands = chart.bands.value;
  const last = chart.points.at(-1);
  let text: ReactNode = "Hover a dotted line or its label on the right to follow one P/E band.";
  const band = bands?.multiples.find((b) => b.multiple === lifted);
  if (bands === null) {
    text = "P/E bands need a positive TTM EPS.";
  } else if (band !== undefined) {
    const m = multipleLabel(band.multiple);
    const vs = band.vsPrice.value;
    text = (
      <>
        <b>{m} band:</b> the share price at {m} TTM EPS.{" "}
        {band.priceToday.value === null || vs === null ? (
          "Today TTM EPS is not positive, so the band has no price."
        ) : (
          <>
            Today that is {usd(last?.ttmEps)} × {String(band.multiple)} ={" "}
            <b>{usd(band.priceToday.value)}</b>, {vsWords[vs]} today&apos;s price of{" "}
            {usd(last?.close)}.
          </>
        )}
      </>
    );
  }
  return (
    <p
      className={band === undefined ? "bandcap" : "bandcap on"}
      data-testid="band-caption"
      data-walk="bandcap"
      aria-live="polite"
    >
      {text}
    </p>
  );
}

/**
 * PriceChartPanel is the price chart: the daily close with overlays for TTM
 * EPS, P/E, the 12-month changes, P/E bands and share-count markers, chips
 * that are legend and toggle, the 6M, 1Y and 3Y ranges and the band caption.
 */
export function PriceChartPanel() {
  const { evaluation: ev, history, profile, dispatch } = useEvaluation();
  const ticker = history.ticker;
  const [stored, setStored] = useView<readonly string[]>(overlaysView, defaultOverlays);
  // The walkthrough shows every overlay without changing the user's choice.
  const [touring, setTouring] = useState(false);
  const storedSet = useMemo(() => overlaySet(stored), [stored]);
  const shown = touring ? everyOverlay : storedSet;
  const [hoverBand, setHoverBand] = useState<number | null>(null);
  const [hover] = useState(createHoverStore);
  const { active } = useWalkthrough();
  const steps = useMemo(() => priceSteps(ticker), [ticker]);
  const chart = ev.priceChart.value;

  const markers = useMemo(
    (): Marker[] =>
      (chart?.shareCountEvents ?? []).map((event) => {
        const q = history.quarters.find((s) => s.label === event.label);
        const src = q?.dilutedShares ?? null;
        return { event, href: src === null ? null : filingLink(src) };
      }),
    [chart, history.quarters],
  );

  const onToggle = (k: OverlayKey) => {
    setStored([...toggled(storedSet, k)]);
  };

  const walkthrough = (
    <Walkthrough
      id={walkthroughId}
      name="the price chart"
      launcher="Walk me through the price chart with my numbers"
      intro="The chart shows whether the share price moved because the company earned more, or because investors became willing to pay more (or less) for the same earnings."
      watchOut="The lines other than the price are stretched to fill the chart, so compare their direction and timing, not their heights. The two 12-month lines are the exception. In a 6M or 1Y range, EPS changes only a few times, so the correlation rests on few reports. Treat it as a hint, not proof."
      steps={steps}
      onStart={() => {
        setTouring(true);
        return () => {
          setTouring(false);
        };
      }}
    />
  );

  // The tip sits beside the range switch, not in the heading, so the panel's name stays "Price".
  const about = (
    <InfoTip label="About price chart">
      Daily closing price, with TTM EPS, P/E and 12-month changes drawn on top. The P/E bands chip
      adds dotted lines: TTM EPS times a fixed multiple. They step up when a new quarter is
      reported. A price that climbs through the bands got more expensive. A price that rises along
      one band rose with earnings. Hover the chart for values.
    </InfoTip>
  );

  const rangeSwitch = (
    <div className={styles.actions}>
      {about}
      <div className={["seg", styles.range].join(" ")} role="group" aria-label="Chart range">
        {ranges.map((r) => (
          <button
            key={r}
            type="button"
            aria-pressed={chart?.range === r}
            onClick={() => {
              dispatch({ type: "setChartRange", range: r });
            }}
          >
            {r}
          </button>
        ))}
      </div>
    </div>
  );

  if (chart === null) {
    return (
      <Panel id="price-chart" title="Price" actions={rangeSwitch} className="chart">
        <p className="panel-soon">The chart needs daily closes, and this stock has none yet.</p>
      </Panel>
    );
  }

  const bandsOn = shown.has("bands") && chart.bands.value !== null;
  const tourBand =
    active?.id === walkthroughId && active.step === bandsStep
      ? (chart.bands.value?.nearest.value ?? null)
      : null;
  const lifted = bandsOn ? (hoverBand ?? tourBand) : null;
  const last = chart.points.at(-1);
  const link = chart.link.value;
  const close = profile.latestClose;
  const period = spanLabel(chart.start, chart.end, chart.range);

  const sub = (
    <span data-testid="price-sub">
      {period.startsWith("since ") ? `Since ${period.slice(6)}` : chart.range}:{" "}
      <span className={chart.priceChange >= 0 ? "up" : "down"}>{signedPct(chart.priceChange)}</span>
      {` · high ${usd(chart.high)} · low ${usd(chart.low)} · P/E ${times(chart.peStart.value)} → ${times(chart.peEnd.value)}`}
      {link !== null && (
        <>
          {" · "}
          <span className={["pxcorr", styles.link].join(" ")} data-walk="link">
            price–earnings link: {link.class} ({link.correlation.toFixed(2)})
          </span>
          <InfoTip label="About the price–earnings link">
            <b>Price–earnings link</b>
            The correlation between the two 12-month lines, on a scale from −1 to +1. It answers:
            when earnings growth sped up or slowed down, did the price change move with it? Strong
            (0.6 or more): it closely tracked earnings growth. Moderate (0.3 to 0.6): in part. Weak
            (below 0.3): hardly, so look for other drivers, such as market mood or interest rates.
            Negative: they tended to move in opposite directions. A correlation does not show what
            caused the moves.
          </InfoTip>
        </>
      )}
      {last !== undefined && (
        <>
          {" · last close "}
          <Sourced value={close !== null && close.asOf === chart.end ? close : null}>
            {usd(last.close)}
          </Sourced>
          {` on ${dayLabel(chart.end)}`}
        </>
      )}
    </span>
  );

  return (
    <Panel id="price-chart" title="Price" sub={sub} actions={rangeSwitch} className="chart">
      <Chips
        points={chart.points}
        hover={hover}
        shown={shown}
        locked={touring}
        onToggle={onToggle}
      />
      {shown.has("bands") && <BandCaption chart={chart} lifted={lifted} />}
      <div className={["px", styles.plot, lifted === null ? "" : "band-hover"].join(" ").trim()}>
        <PricePlot
          chart={chart}
          ticker={ticker}
          shown={shown}
          lifted={lifted}
          onBand={setHoverBand}
          hover={hover}
          markers={markers}
          feed={history.prices?.provider ?? null}
          quarters={history.quarters}
        />
      </div>
      {walkthrough}
    </Panel>
  );
}
