"use client";

import { useMemo } from "react";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import { allMetrics, type Metric } from "@/engine";
import { useEvaluation, useView } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { CompareView } from "./CompareView";
import { MetricChart } from "./MetricChart";
import { metricInfo } from "./metrics";
import { latestFiling, marginSources, metricSources, type LatestFiling } from "./sources";
import { Tiles } from "./Tiles";
import { financialsSteps, intro, watchOut } from "./walkthrough";

/** Shown is what the chart of the panel shows: one metric, or the compare view. */
type Shown = Metric | "compare";

/**
 * FinancialsPanel shows the quarterly financials of the last eight
 * quarters: one metric as a chart or several compared, tiles against a
 * year earlier, and the newest filing. Every figure links to its filing.
 */
export function FinancialsPanel() {
  const { evaluation, history, profile } = useEvaluation();
  const view = evaluation.financials;
  const ticker = profile.ticker.symbol;
  const [stored, setShown] = useView<string>("financials.view", "revenue");
  const shown: Shown =
    stored === "compare" ? "compare" : (allMetrics().find((m) => m === stored) ?? "revenue");
  const sources = useMemo(() => metricSources(view, history.quarters), [view, history.quarters]);
  const margins = useMemo(() => marginSources(view, history.quarters), [view, history.quarters]);
  const filing = useMemo(() => latestFiling(view, history.quarters), [view, history.quarters]);
  const steps = useMemo(() => financialsSteps(ticker), [ticker]);

  const latest = {} as Record<Metric, (typeof sources)[Metric][number]>;
  for (const m of allMetrics()) {
    latest[m] = sources[m].at(-1) ?? null;
  }

  const options: { key: Shown; label: string }[] = [
    ...allMetrics().map((m) => ({ key: m, label: metricInfo[m].short })),
    { key: "compare", label: "Compare" },
  ];
  const actions = (
    <div className="seg seg-wrap" role="group" aria-label="Financial metric">
      {options.map((o) => (
        <button
          key={o.key}
          type="button"
          aria-pressed={shown === o.key}
          data-walk={o.key === "compare" ? "compare-switch" : undefined}
          onClick={() => {
            setShown(o.key);
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  );

  return (
    <Panel
      id="financials"
      title="Financials"
      className="chart"
      sub={
        <>
          Quarterly results from SEC filings, each linked to its filing.
          <InfoTip label="About financials">
            Free cash flow (FCF): operating cash flow minus capital spending. Operating margin:
            operating income divided by revenue. YoY compares a quarter with the same quarter a year
            earlier, because many businesses are seasonal.
          </InfoTip>
        </>
      }
      actions={actions}
    >
      {view.quarters.length === 0 ? (
        <p className="note">The filings of this company give no quarterly financials yet.</p>
      ) : (
        <>
          {shown === "compare" ? (
            <CompareView ticker={ticker} view={view} sources={sources} />
          ) : (
            <MetricChart ticker={ticker} view={view} metric={shown} sources={sources[shown]} />
          )}
          <Tiles view={view} latest={latest} margin={margins.at(-1) ?? null} />
        </>
      )}
      <FilingBox ticker={ticker} cik={profile.company.cik} filing={filing} />
      <Walkthrough
        id="financials"
        name="the financials"
        launcher="Walk me through the financials with my numbers"
        intro={intro}
        watchOut={watchOut}
        steps={steps}
      />
    </Panel>
  );
}

function FilingBox({
  ticker,
  cik,
  filing,
}: {
  ticker: string;
  cik: string;
  filing: LatestFiling | null;
}) {
  const edgar = `https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=${encodeURIComponent(cik)}&type=10-&dateb=&owner=include&count=40`;
  return (
    <div className="filing">
      {filing !== null && (
        <span>
          <span className="eyebrow">Latest filing</span>&nbsp; {ticker} {filing.label} ·{" "}
          <a href={filing.url} target="_blank" rel="noopener noreferrer">
            Form {filing.form}
          </a>
          , filed {filing.filed}
        </span>
      )}
      <span className="muted">
        Every figure keeps its form, filing date and accession number. Hover a figure to see them,
        or click it to open its filing.
      </span>
      <a href={edgar} target="_blank" rel="noopener noreferrer">
        Open filings on EDGAR
      </a>
    </div>
  );
}
