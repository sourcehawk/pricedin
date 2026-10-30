// @vitest-environment node
import {
  bandPrice,
  baselinePath,
  compareScale,
  correlation,
  defaultScenarios,
  earningsPaths,
  estimatedExitPe,
  evaluate,
  exitPeNeeded,
  financials,
  growthScale,
  howDemanding,
  niceStep,
  peBands,
  priceChart,
  project,
  quarterSeries,
  requiredGrowth,
  requirement,
  returnGrid,
  scenarios,
  shareCountEvents,
  tone,
  trailingGrowth,
  trajectoryFill,
  typicalPe,
  verdict,
  weightedTrend,
  yearlyAverage,
  yearRates,
  type ChartRange,
  type CustomGrowth,
  type GridInput,
  type History,
  type Investment,
  type Levers,
  type MaybeNumber,
  type PathsInput,
  type PricePoint,
  type Quarter,
  type ScenarioCase,
} from "./index";
import { compareJson, readJson, toJson, type Json } from "./testing/json";

interface SharedCase {
  name: string;
  fn: string;
  input: Record<string, Json>;
  expected: Json;
  tolerance: number;
  note?: string;
  select?: string;
}

interface CaseFile {
  version: number;
  cases: SharedCase[];
}

/** Input is a case input with its JSON keys; a missing key reads as Go's zero value. */
class Input {
  private readonly used = new Set<string>();

  constructor(private readonly raw: Record<string, Json>) {}

  get(key: string): unknown {
    this.used.add(key);
    return this.raw[key];
  }

  number(key: string): number {
    return (this.get(key) as number | undefined) ?? 0;
  }

  investment(): Investment {
    return {
      price: this.number("price"),
      eps: this.number("eps"),
      targetReturn: this.number("targetReturn"),
      years: this.number("years"),
      dividend: this.number("dividend"),
    };
  }

  /** unused lists the input keys that the runner did not read. */
  unused(): string[] {
    return Object.keys(this.raw).filter((k) => !this.used.has(k));
  }
}

/**
 * runners maps each fn name of the case file to the TypeScript function.
 * A runner reads only the keys of its function's arguments, so a case key
 * that no runner reads fails the case, as it does in the Go runner.
 */
const runners: Record<string, (in_: Input) => unknown> = {
  requiredGrowth: (i) => requiredGrowth(i.investment(), i.number("exitPe")),
  requirement: (i) => requirement(i.investment(), i.number("exitPe")),
  project: (i) => project(i.investment(), i.number("growth"), i.number("exitPe")),
  exitPeNeeded: (i) => exitPeNeeded(i.investment(), i.number("growth")),
  tone: (i) => tone(i.number("diff")),
  verdict: (i) => verdict(i.number("gap")),
  niceStep: (i) => niceStep(i.number("range"), i.number("target")),
  quarterSeries: (i) => quarterSeries(i.get("quarters") as Quarter[]),
  weightedTrend: (i) => weightedTrend(i.get("quarters") as Quarter[], i.number("at")),
  trailingGrowth: (i) => trailingGrowth(i.get("quarters") as Quarter[], i.number("months")),
  trajectoryFill: (i) => trajectoryFill(i.get("quarters") as Quarter[], i.number("years")),
  yearRates: (i) => yearRates(i.get("custom") as CustomGrowth, i.number("years")),
  yearlyAverage: (i) => yearlyAverage(i.get("rates") as number[]),
  baselinePath: (i) =>
    baselinePath(
      i.number("eps"),
      i.number("growth"),
      i.number("quarters"),
      i.get("yearlyRates") as number[] | null,
    ),
  growthScale: (i) => growthScale(i.get("values") as (number | null)[]),
  typicalPe: (i) =>
    typicalPe(
      i.get("quarters") as Quarter[],
      i.number("price"),
      i.number("eps"),
      i.number("months"),
    ),
  estimatedExitPe: (i) =>
    estimatedExitPe(
      i.number("price"),
      i.number("eps"),
      i.number("years"),
      i.get("weightedTrend") as number | null,
      i.get("typicalPe") as number | null,
    ),
  returnGrid: (i) =>
    returnGrid({
      ...i.investment(),
      exitPe: i.number("exitPe"),
      required: i.get("required") as MaybeNumber,
      baseline: i.get("baseline") as GridInput["baseline"],
      weighted: i.get("weighted") as MaybeNumber,
      today: i.get("today") as MaybeNumber,
      trailing: i.get("trailing") as MaybeNumber,
    }),
  earningsPaths: (i) => {
    const input: PathsInput = {
      quarters: i.get("quarters") as Quarter[],
      eps: i.number("eps"),
      years: i.number("years"),
      baseline: i.get("baseline") as MaybeNumber,
      yearlyRates: i.get("yearlyRates") as number[] | null,
      requiredEps: i.get("requiredEps") as MaybeNumber,
    };
    return earningsPaths(input);
  },
  howDemanding: (i) =>
    howDemanding(i.get("quarters") as Quarter[], i.investment(), i.get("exitPe") as MaybeNumber),
  defaultScenarios: (i) =>
    defaultScenarios(i.get("weightedTrend") as number | null, i.get("exitPe") as number | null),
  scenarios: (i) => scenarios(i.investment(), (i.get("cases") as ScenarioCase[] | null) ?? []),
  shareCountEvents: (i) => shareCountEvents(i.get("quarters") as Quarter[]),
  correlation: (i) => correlation(i.get("xs") as number[], i.get("ys") as number[]),
  peBands: (i) => peBands(i.get("points") as PricePoint[]),
  bandPrice: (i) => bandPrice(i.get("ttmEps") as number | null, i.number("multiple")),
  priceChart: (i) =>
    priceChart(
      i.get("daily") as PricePoint[],
      i.get("quarters") as Quarter[],
      i.get("range") as ChartRange | "",
    ),
  financials: (i) => financials(i.get("quarters") as Quarter[]),
  compareScale: (i) => compareScale(i.get("values") as (number | null)[]),
  evaluate: (i) => evaluate(i.get("history") as History, i.get("levers") as Levers),
};

const file = readJson("cases.json") as CaseFile;

describe("shared engine cases", () => {
  it("reads version 1 of the case file", () => {
    expect(file.version).toBe(1);
  });

  it("knows every fn that the case file names", () => {
    const unknown = [...new Set(file.cases.map((c) => c.fn))].filter(
      (fn) => !Object.hasOwn(runners, fn),
    );
    expect(unknown).toEqual([]);
  });

  it("has a case for every runner", () => {
    const named = new Set(file.cases.map((c) => c.fn));
    expect(Object.keys(runners).filter((fn) => !named.has(fn))).toEqual([]);
  });

  it.each(file.cases)("$fn: $name", (c) => {
    const run = Object.hasOwn(runners, c.fn) ? runners[c.fn] : undefined;
    if (run === undefined) {
      throw new Error(`no TypeScript runner for fn "${c.fn}"`);
    }
    const input = new Input(c.input);
    let got = toJson(run(input));
    expect(input.unused(), "input keys that no argument reads").toEqual([]);
    if (c.select !== undefined) {
      if (got === null || typeof got !== "object" || Array.isArray(got)) {
        throw new Error(`select "${c.select}" on a non-object output`);
      }
      got = got[c.select] ?? null;
    }
    const diffs = compareJson(c.expected, got, c.tolerance);
    const shown =
      diffs.length > 10
        ? [...diffs.slice(0, 10), `... and ${String(diffs.length - 10)} more`]
        : diffs;
    expect(shown, `output differs from the oracle (note: ${c.note ?? ""})`).toEqual([]);
  });
});
