import type { RenderResult } from "@testing-library/react";
import type { QuarterSources, SecSource, Source, StockHistory } from "@/api/types";
import { exampleHistory, historyFixture } from "@/api/testing/fixtures";
import { renderReady, WhenReady } from "@/testing/render";
import { FinancialsPanel } from "./FinancialsPanel";

// Test data for the financials panel: the example history with a filing
// for every quarterly value. The numbers are invented demo data.

/** HistoryQuarter is a quarter of the history as the API sends it. */
export type HistoryQuarter = StockHistory["history"]["quarters"][number];
type Q = HistoryQuarter;

/** filedOn is the filing date that the test data gives the quarter that ends on periodEnd. */
export function filedOn(periodEnd: string): string {
  const d = new Date(`${periodEnd}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 30);
  return d.toISOString().slice(0, 10);
}

/** filingUrl is the EDGAR address that the test data gives a filing. */
export function filingUrl(accession: string): string {
  return `https://www.sec.gov/Archives/edgar/data/1121788/${accession.replaceAll("-", "")}/doc.htm`;
}

function sec(q: Q, concept: string, value: number): SecSource {
  const accession = `0001121788-${q.periodEnd.slice(2, 4)}-${q.periodEnd.slice(5, 7)}0001`;
  return {
    kind: "sec",
    form: q.label.startsWith("Q4") ? "10-K" : "10-Q",
    accession,
    filed: filedOn(q.periodEnd),
    concept,
    periodStart: null,
    periodEnd: q.periodEnd,
    reported: value,
    splitRatio: 1,
    scale: 1,
    url: filingUrl(accession),
  };
}

function maybe(v: number | null | undefined, f: (v: number) => Source): Source | null {
  return v === null || v === undefined ? null : f(v);
}

function sourcesOf(q: Q, i: number): QuarterSources {
  return {
    label: q.label,
    periodEnd: q.periodEnd,
    fiscalYear: 2022 + Math.floor(i / 4),
    fiscalQuarter: (i % 4) + 1,
    ttmEpsKnownOn: q.periodEnd,
    eps: maybe(q.eps, (v) => sec(q, "EarningsPerShareDiluted", v)),
    close: null,
    dilutedShares: maybe(q.dilutedShares, (v) =>
      sec(q, "WeightedAverageNumberOfDilutedSharesOutstanding", v),
    ),
    revenue: maybe(q.revenue, (v) => sec(q, "Revenues", v)),
    operatingIncome: maybe(q.operatingIncome, (v) => sec(q, "OperatingIncomeLoss", v)),
    operatingCosts: maybe(q.operatingCosts, () => ({
      kind: "derived",
      method: "derived",
      estimate: false,
      inputs: [
        sec(q, "Revenues", q.revenue ?? 0),
        sec(q, "OperatingIncomeLoss", q.operatingIncome ?? 0),
      ],
    })),
    freeCashFlow: maybe(q.freeCashFlow, (v) => ({
      kind: "derived",
      method: "derived",
      estimate: false,
      inputs: [
        sec(q, "NetCashProvidedByUsedInOperatingActivities", v + 50_000_000),
        sec(q, "PaymentsToAcquirePropertyPlantAndEquipment", 50_000_000),
      ],
    })),
  };
}

/** financialsHistory is the example history, changed by edit, with a filing for every value. */
export function financialsHistory(edit: (qs: Q[]) => Q[] = (qs) => qs): StockHistory {
  const quarters = edit(exampleHistory.quarters.map((q) => ({ ...q })));
  return {
    ...historyFixture,
    history: { ...exampleHistory, quarters },
    quarters: quarters.map((q, i) => sourcesOf(q, i)),
  };
}

/** renderPanel renders the financials panel and waits for the evaluation. */
export function renderPanel(): Promise<RenderResult> {
  return renderReady(
    <WhenReady>
      <FinancialsPanel />
    </WhenReady>,
  );
}
