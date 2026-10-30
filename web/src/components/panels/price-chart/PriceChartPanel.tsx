"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { SourcedNumber } from "@/api/types";
import { coefficient, factor, multiple, signedPct, times, usd } from "@/components/format";
import { filingLink, Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import { leaveDelay } from "@/components/tip/TipProvider";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import { useWalkthrough } from "@/components/walkthrough/WalkthroughProvider";
import type { Chart, ChartRange } from "@/engine";
import { useEvaluation, useView } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { Chips } from "./Chips";
import { dayLabel, spanLabel } from "./geometry";
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
import { closeSourced, ttmSourced } from "./sources";
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

function BandCaption({
  chart,
  lifted,
  close,
  ttmEps,
  onHold,
}: {
  chart: Chart;
  lifted: number | null;
  /** The last close with its source, when it has one. */
  close: SourcedNumber | null;
  /** The last day's TTM EPS with its source, when it has one. */
  ttmEps: SourcedNumber | null;
  /** Gets true while the caption has the pointer or the focus, so the band stays and its sources stay reachable. */
  onHold: (held: boolean) => void;
}) {
  const bands = chart.bands.value;
  const last = chart.points.at(-1);
  let text: ReactNode = "Hover a dotted line or its label on the right to follow one P/E band.";
  const band = bands?.multiples.find((b) => b.multiple === lifted);
  if (bands === null) {
    text = "P/E bands need a positive TTM EPS.";
  } else if (band !== undefined) {
    const m = multiple(band.multiple);
    const vs = band.vsPrice.value;
    text = (
      <>
        <b>{m} band:</b> the share price at {m} TTM EPS.{" "}
        {band.priceToday.value === null || vs === null ? (
          "Today TTM EPS is not positive, so the band has no price."
        ) : (
          <>
            Today that is <Sourced value={ttmEps}>{usd(last?.ttmEps)}</Sourced> ×{" "}
            {factor(band.multiple)} = <b>{usd(band.priceToday.value)}</b>, {vsWords[vs]}{" "}
            today&apos;s price of <Sourced value={close}>{usd(last?.close)}</Sourced>.
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
      onPointerEnter={() => {
        onHold(true);
      }}
      onPointerLeave={() => {
        onHold(false);
      }}
      onFocus={() => {
        onHold(true);
      }}
      onBlur={(e) => {
        if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) {
          onHold(false);
        }
      }}
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
  // The band that the caption keeps while the user reaches for its sources.
  const [captionBand, setCaptionBand] = useState<number | null>(null);
  const leaving = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stay = useCallback(() => {
    if (leaving.current !== null) {
      clearTimeout(leaving.current);
      leaving.current = null;
    }
  }, []);
  useEffect(() => stay, [stay]);
  // A band that the pointer or the focus leaves drops after a short delay,
  // in which the caption above the chart can take it.
  const onBand = useCallback(
    (m: number | null) => {
      stay();
      if (m !== null) {
        setHoverBand(m);
        return;
      }
      leaving.current = setTimeout(() => {
        leaving.current = null;
        setHoverBand(null);
      }, leaveDelay);
    },
    [stay],
  );
  const onHold = (held: boolean) => {
    stay();
    setCaptionBand(held ? (captionBand ?? hoverBand) : null);
    if (!held) {
      setHoverBand(null);
    }
  };
  const [hover] = useState(createHoverStore);
  const { active } = useWalkthrough();
  const steps = useMemo(() => priceSteps(ticker), [ticker]);
  const chart = ev.priceChart.value;

  const markers = useMemo(
    (): Marker[] =>
      (chart?.shareCountEvents ?? []).map((event) => {
        const i = history.quarters.findIndex((s) => s.label === event.label);
        const to = history.quarters[i]?.dilutedShares ?? null;
        const from = i > 0 ? (history.quarters[i - 1]?.dilutedShares ?? null) : null;
        return { event, href: to === null ? null : filingLink(to), sources: { from, to } };
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
  // The bands step explains the nearest band, so a hover does not replace it.
  const lifted = bandsOn ? (tourBand ?? captionBand ?? hoverBand) : null;
  const last = chart.points.at(-1);
  const link = chart.link.value;
  const period = spanLabel(chart.start, chart.end, chart.range);
  const sourcedOn = (date: string, close: number) =>
    closeSourced(date, close, history.quarters, profile.latestClose);
  // The high and the low can close on several days; any of them with a source will do.
  const sourced = (v: number) => {
    for (const p of chart.points) {
      const s = p.close === v ? sourcedOn(p.date, v) : null;
      if (s !== null) {
        return s;
      }
    }
    return null;
  };
  const lastClose = last === undefined ? null : sourcedOn(last.date, last.close);

  const sub = (
    <span data-testid="price-sub">
      {period.startsWith("since ") ? `Since ${period.slice(6)}` : chart.range}:{" "}
      <span className={chart.priceChange >= 0 ? "up" : "down"}>{signedPct(chart.priceChange)}</span>
      {" · high "}
      <Sourced value={sourced(chart.high)}>{usd(chart.high)}</Sourced>
      {" · low "}
      <Sourced value={sourced(chart.low)}>{usd(chart.low)}</Sourced>
      {` · P/E ${times(chart.peStart.value)} → ${times(chart.peEnd.value)}`}
      {link !== null && (
        <>
          {" · "}
          <span className={["pxcorr", styles.link].join(" ")} data-walk="link">
            price–earnings link: {link.class} ({coefficient(link.correlation)})
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
          <Sourced value={lastClose}>{usd(last.close)}</Sourced>
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
      {shown.has("bands") && (
        <BandCaption
          chart={chart}
          lifted={lifted}
          close={lastClose}
          ttmEps={last === undefined ? null : ttmSourced(last, history.quarters)}
          onHold={onHold}
        />
      )}
      <div className={["px", styles.plot, lifted === null ? "" : "band-hover"].join(" ").trim()}>
        <PricePlot
          chart={chart}
          ticker={ticker}
          shown={shown}
          lifted={lifted}
          onBand={onBand}
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
