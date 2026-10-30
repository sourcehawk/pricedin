import { readFileSync } from "node:fs";
import path from "node:path";
import type { Evaluation, Levers } from "@/engine";
import type {
  Profile,
  SecSource,
  Session,
  SourcedNumber,
  Status,
  StockHistory,
  TickerMatch,
} from "../types";

// Test data for the API, built from the engine's GRMN example in
// testdata/engine/evaluation-example.json. Its numbers are invented demo
// data: tests only.

interface Example {
  input: { history: StockHistory["history"]; levers: Levers };
  output: Evaluation;
}

const example = JSON.parse(
  readFileSync(
    path.join(
      import.meta.dirname,
      "..",
      "..",
      "..",
      "..",
      "testdata",
      "engine",
      "evaluation-example.json",
    ),
    "utf8",
  ),
) as Example;

/** exampleHistory is the engine input of the example. */
export const exampleHistory: StockHistory["history"] = example.input.history;
/** exampleLevers are the example's levers: the defaults with the stock's price and TTM EPS. */
export const exampleLevers: Levers = example.input.levers;
/** exampleEvaluation is the engine output for exampleHistory and exampleLevers. */
export const exampleEvaluation: Evaluation = example.output;

const lastQuarter = exampleHistory.quarters.at(-1);
const lastDay = exampleHistory.daily.at(-1);
const closeDate = lastDay?.date ?? "2026-09-30";

function secSource(label: string, periodEnd: string, value: number): SecSource {
  return {
    kind: "sec",
    form: label.startsWith("Q4") ? "10-K" : "10-Q",
    accession: `0000000000-26-${periodEnd.replaceAll("-", "").slice(2)}`,
    filed: periodEnd,
    concept: "EarningsPerShareDiluted",
    periodStart: null,
    periodEnd,
    reported: value,
    splitRatio: 1,
    scale: 1,
    url: `https://www.sec.gov/Archives/edgar/data/0/${periodEnd}.htm`,
  };
}

const ttmInputs = exampleHistory.quarters
  .slice(-4)
  .map((q) => secSource(q.label, q.periodEnd, q.eps ?? 0));

/** sessionFixture is the owner's session. */
export const sessionFixture: Session = {
  email: "owner@example.com",
  createdAt: "2026-10-30T08:00:00Z",
  expiresAt: "2026-11-29T08:00:00Z",
};

/** priceSourced is the latest close with its provider. */
export const priceSourced: SourcedNumber = {
  value: lastDay?.close ?? 270,
  source: {
    kind: "price",
    provider: "massive",
    symbol: "GRMN",
    closeDate,
    rawClose: lastDay?.close ?? 270,
    splitFactor: 1,
  },
  asOf: closeDate,
  asOfLabel: "as of close",
};

/** ttmEpsSourced is the TTM EPS with the four filings it adds up. */
export const ttmEpsSourced: SourcedNumber = {
  value: exampleLevers.ttmEps,
  source: { kind: "derived", method: "ttm", estimate: false, inputs: ttmInputs },
  asOf: lastQuarter?.periodEnd ?? closeDate,
};

/** profileFixture is a loaded stock. */
export const profileFixture: Profile = {
  ticker: { symbol: "GRMN", exchange: "XNYS" },
  company: {
    cik: "0001121788",
    name: "Garmin Ltd.",
    sicCode: "3812",
    sicDescription: "Search, detection, navigation, guidance, aeronautical systems",
    fiscalYearEnd: "1231",
  },
  loaded: true,
  latestClose: priceSourced,
  ttmEps: ttmEpsSourced,
  ttmDividend: {
    value: 0,
    source: { kind: "derived", method: "recurring_dividends_12m", estimate: false, inputs: [] },
    asOf: closeDate,
  },
  pe: {
    value: priceSourced.value / ttmEpsSourced.value,
    source: { kind: "derived", method: "price_over_ttm_eps", estimate: false, inputs: [] },
    asOf: closeDate,
  },
  freshness: { latestFiled: lastQuarter?.periodEnd ?? null, latestClose: closeDate },
};

/** notLoadedProfile is a known company whose data is not loaded yet. */
export const notLoadedProfile: Profile = {
  ...profileFixture,
  ticker: { symbol: "KO", exchange: "XNYS" },
  company: { ...profileFixture.company, cik: "0000021344", name: "Coca-Cola Co" },
  loaded: false,
  ttmEps: null,
  pe: null,
};

/** historyFixture is the history response of the example. */
export const historyFixture: StockHistory = {
  ticker: "GRMN",
  range: "3Y",
  history: exampleHistory,
  quarters: exampleHistory.quarters.map((q, i) => ({
    label: q.label,
    periodEnd: q.periodEnd,
    fiscalYear: 2022 + Math.floor(i / 4),
    fiscalQuarter: (i % 4) + 1,
    ttmEpsKnownOn: q.periodEnd,
    eps: secSource(q.label, q.periodEnd, q.eps ?? 0),
    close: null,
    dilutedShares: null,
    revenue: null,
    operatingIncome: null,
    operatingCosts: null,
    freeCashFlow: null,
  })),
  prices: { provider: "massive", symbol: "GRMN", splits: [] },
  freshness: profileFixture.freshness,
};

/** searchFixture are the matches for "g". */
export const searchFixture: TickerMatch[] = [
  { symbol: "GRMN", name: "Garmin Ltd.", exchange: "XNYS", cik: "0001121788", loaded: true },
  { symbol: "GOOGL", name: "Alphabet Inc.", exchange: "XNAS", cik: "0001652044", loaded: false },
  { symbol: "GE", name: "GE Aerospace", exchange: "XNYS", cik: "0000040545", loaded: true },
];

/** statusFixture is a data page status with one failed run. */
export const statusFixture: Status = {
  latestPriceDate: closeDate,
  companies: { total: 3, withFacts: 2, withFundamentals: 2 },
  ingestRuns: [
    {
      id: 7,
      job: "prices-daily",
      startedAt: "2026-10-29T21:00:00Z",
      finishedAt: "2026-10-29T21:01:30Z",
      status: "succeeded",
      abandoned: false,
      stats: { bars: 5321 },
      error: "",
    },
    {
      id: 8,
      job: "sec-daily",
      startedAt: "2026-10-29T22:00:00Z",
      finishedAt: "2026-10-29T22:00:04Z",
      status: "failed",
      abandoned: false,
      stats: {},
      error: "fetch daily index: 503 Service Unavailable",
    },
    {
      id: 5,
      job: "sec-bulk",
      startedAt: "2026-10-28T02:00:00Z",
      finishedAt: "2026-10-28T02:40:00Z",
      status: "succeeded",
      abandoned: false,
      stats: { facts: 120000 },
      error: "",
    },
    {
      id: 9,
      job: "prices-backfill",
      startedAt: "2026-10-29T23:00:00Z",
      finishedAt: "2026-10-29T23:10:00Z",
      status: "failed",
      abandoned: true,
      stats: {},
      error: "",
    },
  ],
  secRetries: { waiting: 2, exhausted: 1 },
  trackedCompanies: 4,
};
