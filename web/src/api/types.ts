import type { components } from "./schema";

// Short names for the API's schemas. The rest of the app imports these, so
// a renamed schema in the OpenAPI document changes this file only.
type Schemas = components["schemas"];

export type Problem = Schemas["Problem"];
export type Session = Schemas["Session"];
export type TickerMatch = Schemas["TickerMatch"];
export type Source = Schemas["Source"];
export type SecSource = Schemas["SECSource"];
export type PriceSource = Schemas["PriceSource"];
export type DividendSource = Schemas["DividendSource"];
export type DerivedSource = Schemas["DerivedSource"];
export type SourcedNumber = Schemas["SourcedNumber"];
export type Profile = Schemas["Profile"];
export type QuarterSources = Schemas["QuarterSources"];
export type StockHistory = Schemas["StockHistory"];
export type Fundamentals = Schemas["Fundamentals"];
export type EvaluationResult = Schemas["EvaluationResult"];
export type IngestRun = Schemas["IngestRun"];
export type Status = Schemas["Status"];
