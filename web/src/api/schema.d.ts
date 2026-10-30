/**
 * PLACEHOLDER until server/api/openapi.yaml lands with the HTTP API (#10).
 * Hand-written in the shape that openapi-typescript generates, from the
 * endpoint list of #10 and the JSON shapes of server/internal/stocks.
 * `make generate-api-types` replaces this file with the generated one.
 */

import type { Evaluation, History, Levers } from "@/engine";

type Json<T> = { headers: Record<string, unknown>; content: { "application/json": T } };
type ProblemResponse = {
  headers: Record<string, unknown>;
  content: { "application/problem+json": components["schemas"]["Problem"] };
};
type NoParams = { query?: never; header?: never; path?: never; cookie?: never };
type TickerPath = { query?: never; header?: never; path: { ticker: string }; cookie?: never };

export interface paths {
  "/api/auth/login": {
    parameters: NoParams;
    post: operations["login"];
  };
  "/api/auth/logout": {
    parameters: NoParams;
    post: operations["logout"];
  };
  "/api/auth/session": {
    parameters: NoParams;
    get: operations["getSession"];
  };
  "/api/v1/search": {
    parameters: NoParams;
    get: operations["search"];
  };
  "/api/v1/stocks/{ticker}": {
    parameters: TickerPath;
    get: operations["getProfile"];
  };
  "/api/v1/stocks/{ticker}/history": {
    parameters: TickerPath;
    get: operations["getHistory"];
  };
  "/api/v1/stocks/{ticker}/fundamentals": {
    parameters: TickerPath;
    get: operations["getFundamentals"];
  };
  "/api/v1/stocks/{ticker}/evaluation": {
    parameters: TickerPath;
    post: operations["evaluate"];
  };
  "/api/v1/stocks/{ticker}/load": {
    parameters: TickerPath;
    post: operations["loadCompany"];
  };
  "/api/v1/status": {
    parameters: NoParams;
    get: operations["getStatus"];
  };
}

export interface components {
  schemas: {
    Problem: {
      type?: string;
      title: string;
      status: number;
      detail?: string;
      instance?: string;
    };
    LoginRequest: { email: string; password: string };
    Session: { email: string; expiresAt: string };
    TickerMatch: {
      symbol: string;
      name: string;
      exchange: string;
      cik: string;
      loaded: boolean;
    };
    SearchResults: { results: components["schemas"]["TickerMatch"][] };
    SECSource: {
      kind: "sec";
      form: string;
      accession: string;
      filed: string;
      concept: string;
      periodStart: string | null;
      periodEnd: string;
      reported: number;
      splitRatio: number;
      scale: number;
      url: string;
    };
    PriceSource: {
      kind: "price";
      provider: string;
      symbol: string;
      closeDate: string;
      rawClose: number;
      splitFactor: number;
    };
    DividendSource: {
      kind: "dividend";
      provider: string;
      symbol: string;
      exDate: string;
      payDate: string | null;
      amount: number;
      splitRatio: number;
      dividendType: string;
    };
    DerivedSource: {
      kind: "derived";
      method: string;
      estimate: boolean;
      label?: string;
      inputs: components["schemas"]["Source"][];
    };
    UserSource: { kind: "user" };
    Source:
      | components["schemas"]["SECSource"]
      | components["schemas"]["PriceSource"]
      | components["schemas"]["DividendSource"]
      | components["schemas"]["DerivedSource"]
      | components["schemas"]["UserSource"];
    SourcedNumber: {
      value: number;
      source: components["schemas"]["Source"];
      asOf: string;
      asOfLabel?: string;
    };
    Freshness: { latestFiled: string | null; latestClose: string | null };
    Profile: {
      ticker: { symbol: string; exchange: string };
      company: {
        cik: string;
        name: string;
        sicCode: string;
        sicDescription: string;
        fiscalYearEnd: string;
      };
      loaded: boolean;
      latestClose: components["schemas"]["SourcedNumber"] | null;
      ttmEps: components["schemas"]["SourcedNumber"] | null;
      ttmDividend: components["schemas"]["SourcedNumber"];
      pe: components["schemas"]["SourcedNumber"] | null;
      freshness: components["schemas"]["Freshness"];
    };
    QuarterSources: {
      label: string;
      periodEnd: string;
      fiscalYear: number;
      fiscalQuarter: number;
      ttmEpsKnownOn: string | null;
      eps: components["schemas"]["Source"] | null;
      close: components["schemas"]["PriceSource"] | null;
      dilutedShares: components["schemas"]["Source"] | null;
      revenue: components["schemas"]["Source"] | null;
      operatingIncome: components["schemas"]["Source"] | null;
      operatingCosts: components["schemas"]["Source"] | null;
      freeCashFlow: components["schemas"]["Source"] | null;
    };
    StockHistory: {
      ticker: string;
      range: "6M" | "1Y" | "3Y" | "max";
      history: History;
      quarters: components["schemas"]["QuarterSources"][];
      prices: { provider: string; symbol: string } | null;
      freshness: components["schemas"]["Freshness"];
    };
    Fundamentals: {
      ticker: string;
      cik: string;
      quarters: {
        fiscalYear: number;
        fiscalQuarter: number;
        label: string;
        periodStart: string;
        periodEnd: string;
        values: Record<
          string,
          {
            value: number;
            method: string;
            estimate: boolean;
            filed: string;
            firstFiled: string;
            inputs: components["schemas"]["SECSource"][];
          }
        >;
      }[];
      splits: {
        date: string;
        from: number;
        to: number;
        ratio: number;
        source: string;
        evidence: string;
      }[];
      conflicts: { kind: string; concept: string; periodEnd: string }[];
    };
    EvaluationResult: {
      ticker: string;
      levers: Levers;
      sources: {
        price: components["schemas"]["SourcedNumber"];
        ttmEps: components["schemas"]["SourcedNumber"];
        dividend: components["schemas"]["SourcedNumber"];
      };
      asOf: string;
      evaluation: Evaluation;
    };
    IngestRun: {
      id: number;
      job: string;
      startedAt: string;
      finishedAt: string | null;
      status: "running" | "succeeded" | "failed";
      stats: Record<string, number>;
      error: string;
    };
    Status: {
      latestPriceDate: string | null;
      latestFiled: string | null;
      coverage: { companies: number; withFacts: number; withFundamentals: number };
      runs: components["schemas"]["IngestRun"][];
    };
  };
}

export interface operations {
  login: {
    parameters: NoParams;
    requestBody: { content: { "application/json": components["schemas"]["LoginRequest"] } };
    responses: {
      200: Json<components["schemas"]["Session"]>;
      400: ProblemResponse;
      401: ProblemResponse;
      403: ProblemResponse;
      429: ProblemResponse;
    };
  };
  logout: {
    parameters: NoParams;
    requestBody?: never;
    responses: { 204: { headers: Record<string, unknown>; content?: never } };
  };
  getSession: {
    parameters: NoParams;
    requestBody?: never;
    responses: { 200: Json<components["schemas"]["Session"]>; 401: ProblemResponse };
  };
  search: {
    parameters: {
      query: { q: string; limit?: number };
      header?: never;
      path?: never;
      cookie?: never;
    };
    requestBody?: never;
    responses: { 200: Json<components["schemas"]["SearchResults"]>; 401: ProblemResponse };
  };
  getProfile: {
    parameters: TickerPath;
    requestBody?: never;
    responses: {
      200: Json<components["schemas"]["Profile"]>;
      401: ProblemResponse;
      404: ProblemResponse;
    };
  };
  getHistory: {
    parameters: {
      query?: { range?: "6M" | "1Y" | "3Y" | "max" };
      header?: never;
      path: { ticker: string };
      cookie?: never;
    };
    requestBody?: never;
    responses: {
      200: Json<components["schemas"]["StockHistory"]>;
      401: ProblemResponse;
      404: ProblemResponse;
      409: ProblemResponse;
    };
  };
  getFundamentals: {
    parameters: TickerPath;
    requestBody?: never;
    responses: {
      200: Json<components["schemas"]["Fundamentals"]>;
      401: ProblemResponse;
      404: ProblemResponse;
    };
  };
  evaluate: {
    parameters: TickerPath;
    requestBody: { content: { "application/json": Partial<Levers> } };
    responses: {
      200: Json<components["schemas"]["EvaluationResult"]>;
      400: ProblemResponse;
      401: ProblemResponse;
      404: ProblemResponse;
    };
  };
  loadCompany: {
    parameters: TickerPath;
    requestBody?: never;
    responses: {
      200: Json<components["schemas"]["Profile"]>;
      401: ProblemResponse;
      404: ProblemResponse;
      501: ProblemResponse;
    };
  };
  getStatus: {
    parameters: NoParams;
    requestBody?: never;
    responses: { 200: Json<components["schemas"]["Status"]>; 401: ProblemResponse };
  };
}
