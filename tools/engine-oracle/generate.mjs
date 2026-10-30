// Writes testdata/engine/cases.json and testdata/engine/evaluation-example.json
// from the oracle. Run it with `make generate-engine-cases`.
//
// Usage: node tools/engine-oracle/generate.mjs [output directory]

import { strict as assert } from "node:assert";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { defaultLevers, profile } from "./fixtures.mjs";
import * as O from "./oracle.mjs";
import * as P from "./prototype.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(process.argv[2] || join(here, "..", "..", "testdata", "engine"));

const N = O.NOTES;
const TOL = 1e-9;
const cases = [];

// add evaluates fn on input with the oracle and records the case.
function add(name, fn, input, note) {
  const expected = O[fn](structuredClone(input));
  const c = { name, fn, input, expected: clean(expected), tolerance: TOL };
  if (note) c.note = Array.isArray(note) ? note.join(" ") : note;
  cases.push(c);
}

// hand records a case whose expected value is written by hand.
function hand(name, fn, input, expected, tolerance, note, select) {
  const c = { name, fn, input, expected, tolerance };
  if (select) c.select = select;
  if (note) c.note = note;
  cases.push(c);
}

// clean drops undefined fields and fails on NaN or Infinity, which JSON
// cannot carry.
function clean(v, path = "$") {
  if (typeof v === "number") {
    assert.ok(Number.isFinite(v), `non-finite number at ${path}`);
    return v === 0 ? 0 : v;
  }
  if (Array.isArray(v)) return v.map((x, i) => clean(x, `${path}[${i}]`));
  if (v && typeof v === "object") {
    const o = {};
    for (const [k, x] of Object.entries(v)) if (x !== undefined) o[k] = clean(x, `${path}.${k}`);
    return o;
  }
  return v;
}

// format writes JSON with each value on one line when it fits in 100
// characters, so that the files stay small and their diffs stay readable.
function format(v, indent = "") {
  const flat = JSON.stringify(v);
  if (flat.length + indent.length <= 100 || v === null || typeof v !== "object") return flat;
  const inner = indent + " ";
  if (Array.isArray(v))
    return `[\n${v.map((x) => inner + format(x, inner)).join(",\n")}\n${indent}]`;
  const items = Object.entries(v).map(
    ([k, x]) => `${inner}${JSON.stringify(k)}: ${format(x, inner)}`,
  );
  return `{\n${items.join(",\n")}\n${indent}}`;
}

// ------------------------------------------------------------- fixtures

const GRMN = profile("GRMN", { weekly: true });
const GRMN_FULL = profile("GRMN");
const NKE = profile("NKE", { weekly: true });
const KO = profile("KO", { weekly: true });
const PLTR = profile("PLTR", { weekly: true });
const AMD = profile("AMD", { weekly: true });
const MSFT = profile("MSFT", { weekly: true });
const gq = GRMN.history.quarters;

const withEps = (qs, idx, eps) => qs.map((q, i) => (i === idx ? { ...q, eps } : q));
const short = (qs, k) => qs.slice(qs.length - k);
const missing = withEps(gq, 14, null);
const lossQuarters = withEps(withEps(gq, 9, -0.4), 13, -0.2);

// The oracle must agree with the prototype's own loadData on complete data.
for (const p of [GRMN, NKE, KO, PLTR, AMD, MSFT]) {
  const qs = p.history.quarters;
  const proto = P.loadData(qs.map((q) => [q.label, q.eps, q.close]));
  const s = O.series(qs);
  assert.deepEqual(s.ttmN, proto.ttm);
  assert.deepEqual(s.qyoyN, proto.qyoy);
  assert.deepEqual(s.tyoyN, proto.tyoy);
  assert.equal(s.WTD, proto.WTD);
}

// ------------------------------------------------------------ core math

const ref = { price: 270, eps: 10.8, years: 3, dividend: 0 };
hand(
  "reference: GRMN 15% target at 20x needs 23.9% growth",
  "requiredGrowth",
  { ...ref, targetReturn: 0.15, exitPe: 20 },
  { value: 0.239 },
  5e-4,
  "Hand-written reference value from CLAUDE.md, rounded to 0.1%.",
);
hand(
  "reference: GRMN 10% target at 22x needs 14.8% growth",
  "requiredGrowth",
  { ...ref, targetReturn: 0.1, exitPe: 22 },
  { value: 0.148 },
  5e-4,
  "Hand-written reference value from CLAUDE.md, rounded to 0.1%.",
);
hand(
  "reference: GRMN 12% growth at 22x exits at $334",
  "project",
  { ...ref, growth: 0.12, exitPe: 22 },
  334,
  0.5,
  "Hand-written reference value from CLAUDE.md, rounded to the dollar.",
  "exitPrice",
);
hand(
  "reference: GRMN 12% growth at 22x returns about 7.3% a year",
  "project",
  { ...ref, growth: 0.12, exitPe: 22 },
  0.073,
  5e-4,
  "Hand-written reference value from CLAUDE.md, rounded to 0.1%.",
  "yearlyReturn",
);

const rq = { price: 270, eps: 10.8, targetReturn: 0.15, years: 3, exitPe: 22, dividend: 0 };
add("required growth at the default levers", "requiredGrowth", rq);
add("required growth over one year", "requiredGrowth", { ...rq, years: 1 });
add("required growth over ten years", "requiredGrowth", { ...rq, years: 10 });
add("required growth with a dividend", "requiredGrowth", { ...rq, dividend: 3.6 });
add("required growth is negative for a cheap stock", "requiredGrowth", { ...rq, price: 50 });
add("required growth needs positive EPS: zero", "requiredGrowth", { ...rq, eps: 0 });
add("required growth needs positive EPS: negative", "requiredGrowth", { ...rq, eps: -1.2 });
add("required growth needs a positive exit P/E: zero", "requiredGrowth", { ...rq, exitPe: 0 });
add("required growth needs a positive exit P/E: negative", "requiredGrowth", { ...rq, exitPe: -5 });
add(
  "dividends alone reach the target",
  "requiredGrowth",
  { ...rq, price: 50, dividend: 30 },
  N.dividendsReachTarget,
);
add(
  "dividends exactly reach the target",
  "requiredGrowth",
  { price: 100, eps: 5, targetReturn: 0.25, years: 1, exitPe: 15, dividend: 125 },
  N.dividendsReachTarget,
);

add("requirement at the default levers", "requirement", rq);
add("requirement with a dividend over five years", "requirement", {
  ...rq,
  years: 5,
  dividend: 3.6,
});
add("requirement needs positive EPS", "requirement", { ...rq, eps: -0.5 });
add(
  "requirement at a huge exit P/E",
  "requirement",
  { price: 100, eps: 4, targetReturn: 0.15, years: 1, exitPe: 7.38e25, dividend: 0 },
  N.requiredEndpoints,
);
add(
  "required growth rounds to -100% at a huge exit P/E",
  "requiredGrowth",
  { price: 100, eps: 4, targetReturn: 0.15, years: 1, exitPe: 7.38e25, dividend: 0 },
  N.requiredEndpoints,
);
add(
  "requirement when dividends reach the target",
  "requirement",
  { ...rq, price: 50, dividend: 30 },
  N.dividendsReachTarget,
);

const pj = { price: 270, eps: 10.8, growth: 0.12, years: 3, exitPe: 22, dividend: 0 };
add("projection at 12% growth", "project", pj);
add("projection with shrinking EPS", "project", { ...pj, growth: -0.2 });
add("projection when EPS falls to zero with dividends", "project", {
  ...pj,
  growth: -1,
  dividend: 5,
});
add("projection over ten years with a dividend", "project", { ...pj, years: 10, dividend: 3.6 });
add("projection over one year", "project", { ...pj, years: 1 });

const pn = { price: 270, eps: 10.8, targetReturn: 0.15, years: 3, growth: 0.12, dividend: 0 };
add("exit P/E needed at 12% growth", "exitPeNeeded", pn);
add("exit P/E needed with a dividend", "exitPeNeeded", { ...pn, dividend: 3.6 });
add("exit P/E needed needs positive EPS", "exitPeNeeded", { ...pn, eps: 0 });
add(
  "exit P/E needed rejects growth of -100%",
  "exitPeNeeded",
  { ...pn, growth: -1 },
  N.growthRange,
);
add(
  "exit P/E needed when dividends reach the target",
  "exitPeNeeded",
  { ...pn, price: 50, dividend: 30 },
  N.dividendsReachTarget,
);

for (const d of [0.02, 0.015, 0.0150001, 0, -0.015, -0.0150001, -0.3])
  add(`tone of ${d}`, "tone", { diff: d });
add("tone at a computed 1.5 pp gap", "tone", { diff: 0.165 - 0.15 }, N.toneTolerance);
add("tone at a computed -1.5 pp gap", "tone", { diff: 0.135 - 0.15 }, N.toneTolerance);
for (const d of [0.05, 0.015, 0, -0.015, -0.02])
  add(`verdict at a gap of ${d}`, "verdict", { gap: d });
for (const [r, t] of [
  [0, 5],
  [-1, 5],
  [12.3, 5],
  [0.07, 5],
  [1, 5],
  [150, 5],
  [3.2, 5],
  [0.0004, 5],
])
  add(`nice step for ${r}`, "niceStep", { range: r, target: t });

// ------------------------------------------------------------ the series

add("quarterly series of the GRMN demo profile", "quarterSeries", { quarters: gq });
add("quarterly series with a missing EPS", "quarterSeries", { quarters: missing }, N.nullData);
add("quarterly series with losses", "quarterSeries", { quarters: lossQuarters });
add("quarterly series of five quarters", "quarterSeries", { quarters: short(gq, 5) });
add("quarterly series of no quarters", "quarterSeries", { quarters: [] });
add("weighted trend today", "weightedTrend", { quarters: gq, at: 18 });
add("weighted trend a year ago", "weightedTrend", { quarters: gq, at: 14 });
add("weighted trend before any YoY exists", "weightedTrend", { quarters: gq, at: 3 });
add(
  "weighted trend with a missing quarter",
  "weightedTrend",
  { quarters: missing, at: 18 },
  N.nullData,
);

for (const m of [12, 13, 36, 45, 3, 0])
  add(`trailing growth over ${m} months`, "trailingGrowth", { quarters: gq, months: m });
add(
  "trailing growth is limited by the history",
  "trailingGrowth",
  { quarters: gq, months: 60 },
  N.trailingHistory,
);
add(
  "trailing growth with eight quarters of history",
  "trailingGrowth",
  { quarters: short(gq, 8), months: 24 },
  N.trailingHistory,
);
add(
  "trailing growth with too little history",
  "trailingGrowth",
  { quarters: short(gq, 6), months: 12 },
  N.trailingHistory,
);
add(
  "trailing growth with a missing quarter",
  "trailingGrowth",
  { quarters: missing, months: 12 },
  N.nullData,
);
add("trailing growth needs positive TTM EPS", "trailingGrowth", {
  quarters: withEps(withEps(gq, 12, -3), 11, -3),
  months: 12,
});

for (const y of [3, 1, 10])
  add(`trajectory fill of GRMN over ${y} years`, "trajectoryFill", { quarters: gq, years: y });
add("trajectory fill of a slowing company", "trajectoryFill", {
  quarters: NKE.history.quarters,
  years: 5,
});
add("trajectory fill of a fast grower", "trajectoryFill", {
  quarters: PLTR.history.quarters,
  years: 5,
});
add("trajectory fill of a speeding-up company", "trajectoryFill", {
  quarters: AMD.history.quarters,
  years: 4,
});
add("trajectory fill without a weighted trend", "trajectoryFill", {
  quarters: short(gq, 4),
  years: 3,
});
add(
  "trajectory fill when the highest reading is below -50%",
  "trajectoryFill",
  { quarters: withEps(short(gq, 5), 4, 0.245), years: 3 },
  N.trajectoryBounds,
);

add(
  "year rates repeat the last rate",
  "yearRates",
  { custom: { yearly: true, rate: 0.1, rates: [0.2, 0.15] }, years: 4 },
  N.yearRatesPad,
);
add(
  "year rates start from the flat rate",
  "yearRates",
  { custom: { yearly: true, rate: 0.08, rates: [] }, years: 3 },
  N.yearRatesPad,
);
add("year rates are cut to the holding period", "yearRates", {
  custom: { yearly: true, rate: 0.1, rates: [0.3, 0.2, 0.1, 0.05] },
  years: 2,
});
add("yearly average of three rates", "yearlyAverage", { rates: [0.1, 0.2, 0.05] });
add("yearly average of one rate", "yearlyAverage", { rates: [0.07] });
add("yearly average of a loss and a gain", "yearlyAverage", { rates: [-0.5, 0.5] });
hand(
  "yearly average of no rates",
  "yearlyAverage",
  { rates: [] },
  0,
  TOL,
  "v1: the compound average of no rates is 0; the prototype never averages an empty list.",
);
for (const k of [0, 6, 12])
  add(`flat baseline path after ${k} quarters`, "baselinePath", {
    eps: 10.8,
    growth: 0.12,
    quarters: k,
    yearlyRates: null,
  });
for (const k of [0, 5, 8, 12])
  add(`year-by-year baseline path after ${k} quarters`, "baselinePath", {
    eps: 10.8,
    growth: 0.1,
    quarters: k,
    yearlyRates: [0.3, -0.1, 0.15],
  });

add("growth scale of the GRMN levers", "growthScale", {
  values: [0.149, 0.0577, 0.1298, 0.1186, 0.1298],
});
add("growth scale with missing values", "growthScale", { values: [null, null, 0.05, null, 0.05] });
add("growth scale of a fast grower", "growthScale", { values: [0.62, 0.95, 0.71, 0.5, 0.71] });
add("growth scale of a shrinking company", "growthScale", {
  values: [0.3, -0.35, -0.22, -0.1, -0.22],
});
add(
  "growth scale of a growth near 999999",
  "growthScale",
  { values: [0.15, 999999, 999999, null, 999999] },
  N.rulerStep,
);

// ---------------------------------------------------------------- exit P/E

add("typical P/E over 36 months", "typicalPe", { quarters: gq, price: 270, eps: 10.8, months: 36 });
add(
  "typical P/E over 33 months has an even count",
  "typicalPe",
  { quarters: gq, price: 270, eps: 10.8, months: 33 },
  N.typicalMedian,
);
add(
  "typical P/E over 6 months",
  "typicalPe",
  { quarters: gq, price: 270, eps: 10.8, months: 6 },
  N.typicalMedian,
);
add(
  "typical P/E rounds the months",
  "typicalPe",
  { quarters: gq, price: 270, eps: 10.8, months: 20 },
  N.typicalMedian,
);
add("typical P/E caps the months at 36", "typicalPe", {
  quarters: gq,
  price: 270,
  eps: 10.8,
  months: 60,
});
add(
  "typical P/E with a short history",
  "typicalPe",
  { quarters: short(gq, 5), price: 270, eps: 10.8, months: 36 },
  [N.typicalHistory, N.typicalMedian],
);
add(
  "typical P/E without a positive EPS today",
  "typicalPe",
  { quarters: gq, price: 270, eps: -1, months: 12 },
  N.typicalMedian,
);
add(
  "typical P/E without any value",
  "typicalPe",
  { quarters: short(gq, 2), price: 270, eps: -1, months: 12 },
  N.typicalHistory,
);
add(
  "typical P/E with no quarters",
  "typicalPe",
  { quarters: [], price: 100, eps: 5, months: 36 },
  N.typicalToday,
);

const ep = { price: 270, eps: 10.8, years: 3, weightedTrend: 0.1298, typicalPe: 22.1 };
add("estimated exit P/E fades a premium", "estimatedExitPe", ep);
add("estimated exit P/E over ten years", "estimatedExitPe", { ...ep, years: 10 });
add("estimated exit P/E of a slow grower above its typical P/E", "estimatedExitPe", {
  ...ep,
  price: 70,
  eps: 2.95,
  weightedTrend: 0.05,
  typicalPe: 21,
});
add("estimated exit P/E of a slow grower near its typical P/E", "estimatedExitPe", {
  ...ep,
  price: 70,
  eps: 2.95,
  weightedTrend: 0.05,
  typicalPe: 23.5,
});
add("estimated exit P/E keeps a low P/E", "estimatedExitPe", {
  ...ep,
  price: 120,
  weightedTrend: 0.2,
});
add("estimated exit P/E of a shrinking company", "estimatedExitPe", {
  ...ep,
  price: 70,
  eps: 2.1,
  weightedTrend: -0.2,
  typicalPe: 30,
});
add("estimated exit P/E never goes below 6", "estimatedExitPe", {
  ...ep,
  price: 100,
  eps: 10,
  weightedTrend: 0.01,
  typicalPe: 4,
});
add("estimated exit P/E needs positive EPS", "estimatedExitPe", { ...ep, eps: 0 });
add(
  "estimated exit P/E just above the growth floor",
  "estimatedExitPe",
  { price: 300, eps: 10, years: 3, weightedTrend: 0.060000000000000005, typicalPe: 25 },
  N.fadeFactor,
);
add(
  "estimated exit P/E without a weighted trend",
  "estimatedExitPe",
  { ...ep, weightedTrend: null },
  N.noTrend,
);
add("estimated exit P/E without a typical P/E", "estimatedExitPe", {
  ...ep,
  price: 70,
  eps: 2.95,
  weightedTrend: 0.05,
  typicalPe: null,
});

// ---------------------------------------------------------------- the grid

const gridBase = {
  price: 270,
  eps: 10.8,
  targetReturn: 0.15,
  years: 3,
  dividend: 0,
  exitPe: 22,
  required: { value: 0.14953 },
  baseline: { mode: "weighted", growth: { value: 0.12981 } },
  weighted: { value: 0.12981 },
  today: { value: 0.12245 },
  trailing: { value: 0.11869 },
};
add("return grid with the weighted trend", "returnGrid", gridBase, N.exactRow);
add(
  "return grid with today's growth",
  "returnGrid",
  { ...gridBase, baseline: { mode: "today", growth: { value: 0.12245 } } },
  N.exactRow,
);
add(
  "return grid with trailing growth",
  "returnGrid",
  { ...gridBase, baseline: { mode: "trailing", growth: { value: 0.11869 } } },
  N.exactRow,
);
add(
  "return grid with a custom rate on a row",
  "returnGrid",
  { ...gridBase, baseline: { mode: "custom", growth: { value: 0.1 } } },
  N.exactRow,
);
add(
  "return grid with a custom rate between rows",
  "returnGrid",
  { ...gridBase, baseline: { mode: "custom", growth: { value: 0.333 } } },
  N.exactRow,
);
add(
  "return grid when dividends reach the target",
  "returnGrid",
  {
    ...gridBase,
    dividend: 30,
    price: 50,
    required: { value: null, reason: "dividends_reach_target" },
  },
  [N.exactRow, N.dividendsReachTarget],
);
add(
  "return grid without a baseline",
  "returnGrid",
  {
    ...gridBase,
    baseline: { mode: "trailing", growth: { value: null, reason: "insufficient_history" } },
    trailing: { value: null, reason: "insufficient_history" },
  },
  N.exactRow,
);
add(
  "return grid of a fast grower over ten years",
  "returnGrid",
  {
    ...gridBase,
    years: 10,
    exitPe: 150,
    price: 180,
    eps: 0.6,
    required: { value: 0.52 },
    baseline: { mode: "weighted", growth: { value: 0.71 } },
    weighted: { value: 0.71 },
    today: { value: 0.95 },
    trailing: { value: 0.66 },
  },
  N.exactRow,
);
add(
  "return grid of a shrinking company",
  "returnGrid",
  {
    ...gridBase,
    exitPe: 18,
    price: 70,
    eps: 2.1,
    required: { value: 0.31 },
    baseline: { mode: "weighted", growth: { value: -0.62 } },
    weighted: { value: -0.62 },
    today: { value: -0.7 },
    trailing: { value: -0.4 },
  },
  N.exactRow,
);
add("return grid needs positive EPS", "returnGrid", { ...gridBase, eps: 0 });
add(
  "return grid at an exit P/E of 0.5",
  "returnGrid",
  { ...gridBase, exitPe: 0.5, required: { value: 2.1 } },
  [N.exactRow, N.positiveColumns],
);
add(
  "return grid with a baseline above 1000%",
  "returnGrid",
  { ...gridBase, baseline: { mode: "weighted", growth: { value: 11 } }, weighted: { value: 11 } },
  [N.exactRow, N.growthRange],
);

// ---------------------------------------------------------------- the paths

const pathBase = {
  quarters: gq,
  eps: 10.8,
  years: 3,
  baseline: { value: 0.12981 },
  yearlyRates: null,
  requiredEps: { value: 16.4041 },
};
add("earnings paths with the weighted trend", "earningsPaths", pathBase, N.scaleEveryPoint);
add(
  "earnings paths with year-by-year rates",
  "earningsPaths",
  { ...pathBase, baseline: { value: 0.0931 }, yearlyRates: [0.4, -0.2, 0.15] },
  N.scaleEveryPoint,
);
add(
  "earnings paths over one year",
  "earningsPaths",
  { ...pathBase, years: 1, requiredEps: { value: 12.4149 } },
  N.scaleEveryPoint,
);
add(
  "earnings paths without a requirement",
  "earningsPaths",
  { ...pathBase, requiredEps: { value: null, reason: "dividends_reach_target" } },
  [N.scaleEveryPoint, N.dividendsReachTarget],
);
add(
  "earnings paths with losses in the history",
  "earningsPaths",
  { ...pathBase, quarters: withEps(withEps(withEps(gq, 8, -6), 9, -5), 10, -4) },
  N.scaleEveryPoint,
);
add("earnings paths need positive EPS", "earningsPaths", { ...pathBase, eps: -1 });
add(
  "earnings paths to a near-zero required EPS",
  "earningsPaths",
  { ...pathBase, years: 1, requiredEps: { value: 1.5e-24 } },
  [N.scaleEveryPoint, N.requiredEndpoints],
);

const hdBase = {
  quarters: gq,
  price: 270,
  eps: 10.8,
  targetReturn: 0.15,
  years: 3,
  exitPe: { value: 22 },
  dividend: 0,
};
add("how demanding over time", "howDemanding", hdBase, [N.medianRequired, N.demandingCount]);
add("how demanding over time with a dividend", "howDemanding", { ...hdBase, dividend: 3.6 }, [
  N.medianRequired,
  N.demandingCount,
]);
add(
  "how demanding over time without an exit P/E",
  "howDemanding",
  { ...hdBase, exitPe: { value: null, reason: "needs_positive_eps" } },
  N.demandingCount,
);
add(
  "how demanding over time with a short history",
  "howDemanding",
  { ...hdBase, quarters: short(gq, 6) },
  [N.medianRequired, N.demandingCount],
);
add(
  "how demanding over time with a missing quarter",
  "howDemanding",
  { ...hdBase, quarters: missing },
  [N.nullData, N.medianRequired, N.demandingCount],
);

// ------------------------------------------------------------- scenarios

add("default cases around the weighted trend", "defaultScenarios", {
  weightedTrend: 0.12981,
  exitPe: 22,
});
add("default cases of a shrinking company", "defaultScenarios", {
  weightedTrend: -0.22,
  exitPe: 7,
});
add(
  "default cases without a trend or exit P/E",
  "defaultScenarios",
  { weightedTrend: null, exitPe: null },
  N.defaultScenNoTrend,
);
const scenBase = {
  price: 270,
  eps: 10.8,
  targetReturn: 0.15,
  years: 3,
  dividend: 0,
  cases: O.defaultScenarios({ weightedTrend: 0.12981, exitPe: 22 }),
};
add("bear, base and bull cases", "scenarios", scenBase);
add("cases where only the bear case meets the target", "scenarios", {
  ...scenBase,
  cases: [
    { name: "Bear", growth: 0.3, exitPe: 30 },
    { name: "Base", growth: 0.05, exitPe: 15 },
    { name: "Bull", growth: 0.1, exitPe: 18 },
  ],
});
add("cases with a dividend", "scenarios", { ...scenBase, dividend: 3.6, years: 5 });
add("cases need positive EPS", "scenarios", { ...scenBase, eps: 0 });
add(
  "a case that lands exactly on the target",
  "scenarios",
  {
    price: 100,
    eps: 5,
    targetReturn: 0.15,
    years: 1,
    dividend: 0,
    cases: [{ name: "Base", growth: 0.15, exitPe: 20 }],
  },
  N.targetTolerance,
);
add(
  "cases around a collapse of more than -100%",
  "scenarios",
  { ...scenBase, cases: O.defaultScenarios({ weightedTrend: -2.2, exitPe: 10 }) },
  N.scenarioRange,
);
add(
  "cases with a non-positive exit P/E",
  "scenarios",
  {
    ...scenBase,
    cases: [
      { name: "Bear", growth: 0.05, exitPe: 0 },
      { name: "Base", growth: 0.1, exitPe: -5 },
      { name: "Bull", growth: 0.2, exitPe: 25 },
    ],
  },
  N.scenarioRange,
);

// ------------------------------------------------------- share counts

add("share-count events of GRMN", "shareCountEvents", { quarters: gq });
add("share-count events of AMD", "shareCountEvents", { quarters: AMD.history.quarters });
add("share-count events of PLTR", "shareCountEvents", { quarters: PLTR.history.quarters });
add(
  "share-count events skip missing counts",
  "shareCountEvents",
  { quarters: gq.map((q, i) => (i === 14 ? { ...q, dilutedShares: null } : q)) },
  N.nullData,
);

// ------------------------------------------------------------ price chart

const lin = (n, f) => Array.from({ length: n }, (_, i) => f(i));
add("correlation of two linked series", "correlation", {
  xs: lin(24, (i) => i * 0.01),
  ys: lin(24, (i) => i * 0.02 + Math.sin(i) * 0.05),
});
add("correlation of opposite series", "correlation", {
  xs: lin(20, (i) => i * 0.01),
  ys: lin(20, (i) => -i * 0.03 + Math.cos(i) * 0.01),
});
add("correlation needs 20 points", "correlation", { xs: lin(19, (i) => i), ys: lin(19, (i) => i) });
add(
  "correlation of a constant series",
  "correlation",
  { xs: lin(25, () => 0.1), ys: lin(25, (i) => i) },
  N.constantSeries,
);
add(
  "correlation of two constant series",
  "correlation",
  { xs: lin(25, () => 0.1), ys: lin(25, () => 0.2) },
  N.constantSeries,
);
add("correlation of weakly linked series", "correlation", {
  xs: lin(30, (i) => Math.sin(i)),
  ys: lin(30, (i) => Math.cos(i * 1.7)),
});

const tail = (d, k) => d.slice(d.length - k);
add("P/E bands over the last year", "peBands", {
  points: tail(GRMN.history.daily, 52),
});
add("P/E bands over three years", "peBands", { points: GRMN.history.daily });
add(
  "P/E bands without today's EPS",
  "peBands",
  {
    points: tail(GRMN.history.daily, 52).map((d, i, a) =>
      i === a.length - 1 ? { ...d, ttmEps: null } : d,
    ),
  },
  N.nullData,
);
add("band price on a day", "bandPrice", { ttmEps: 10.8, multiple: 22.5 });
add("band price without EPS", "bandPrice", { ttmEps: null, multiple: 20 });
add("band price at a loss", "bandPrice", { ttmEps: -1, multiple: 20 });
const nearZero = (eps) =>
  lin(10, (i) => ({
    date: `2026-01-${String(i + 1).padStart(2, "0")}`,
    close: 90 + 2 * i,
    ttmEps: eps,
  }));
add("P/E bands at a near-zero EPS", "peBands", { points: nearZero(1e-9) }, N.bandSteps);
add("P/E bands at a tiny EPS", "peBands", { points: nearZero(1e-16) }, N.bandSteps);
add("P/E bands at a P/E near 1", "peBands", { points: nearZero(80) }, N.bandSteps);
add(
  "P/E bands at the lowest valid P/E",
  "peBands",
  { points: [{ date: "2026-01-02", close: 1e-4, ttmEps: 1e6 }] },
  [N.bandSteps, N.bandDigits, N.bandSlack],
);
add(
  "P/E bands at a P/E near 0.001",
  "peBands",
  { points: nearZero(10).map((d, i) => ({ ...d, close: 0.01 + 0.001 * i })) },
  [N.bandSteps, N.bandDigits],
);
add("P/E bands need a positive EPS", "peBands", {
  points: tail(GRMN.history.daily, 10).map((d) => ({ ...d, ttmEps: -1 })),
});

for (const r of ["3Y", "1Y", "6M"])
  add(
    `price chart over ${r}`,
    "priceChart",
    { daily: GRMN.history.daily, quarters: gq, range: r },
    [N.g12, N.dailyPe],
  );
add(
  "price chart of a slowing company",
  "priceChart",
  { daily: NKE.history.daily, quarters: NKE.history.quarters, range: "3Y" },
  [N.g12, N.dailyPe],
);
add(
  "price chart with gaps in the EPS",
  "priceChart",
  {
    daily: GRMN.history.daily.map((d, i) => (i % 17 === 3 ? { ...d, ttmEps: null } : d)),
    quarters: gq,
    range: "1Y",
  },
  [N.g12, N.dailyPe, N.nullData],
);
add("price chart without daily data", "priceChart", { daily: [], quarters: gq, range: "3Y" });
const days = (n, f) =>
  lin(n, (i) => ({ date: new Date(Date.UTC(2025, 0, 1 + i)).toISOString().slice(0, 10), ...f(i) }));
add(
  "price chart when EPS fell to zero",
  "priceChart",
  {
    daily: days(400, (i) => ({ close: 100 + (i % 7), ttmEps: i === 399 ? 0 : 5 })),
    quarters: [],
    range: "1Y",
  },
  [N.g12, N.dailyPe, N.peChangeNeedsEps],
);
add(
  "price chart when EPS turned negative",
  "priceChart",
  {
    daily: days(400, (i) => ({ close: 100 + (i % 7), ttmEps: i === 399 ? -2 : 5 })),
    quarters: [],
    range: "6M",
  },
  [N.g12, N.dailyPe, N.peChangeNeedsEps],
);

// ------------------------------------------------------------- financials

add("financials of GRMN", "financials", { quarters: gq }, [N.pairwise, N.barScale]);
add("financials of AMD", "financials", { quarters: AMD.history.quarters }, [
  N.pairwise,
  N.barScale,
]);
add("financials with three quarters", "financials", { quarters: short(gq, 3) }, [
  N.pairwise,
  N.barScale,
]);
add(
  "financials with a negative and a missing value",
  "financials",
  {
    quarters: gq.map((q, i) =>
      i === 15
        ? { ...q, freeCashFlow: -120e6 }
        : i === 17
          ? { ...q, revenue: null, operatingCosts: null }
          : q,
    ),
  },
  [N.pairwise, N.barScale, N.baseCheck, N.nullData],
);
add("compare scale of several series", "compareScale", { values: [0.1, -0.05, 0.32, null, 0.18] });
add("compare scale of no values", "compareScale", { values: [] }, N.zeroWidth);
add("compare scale of one large change", "compareScale", { values: [2.4] });

// --------------------------------------------------------------- evaluate

const ev = (p, over) => ({ history: p.history, levers: defaultLevers(p, over) });
const noDaily = (p) => ({ ...p, history: { ...p.history, daily: [] } });
const evalNotes = [
  N.typicalMedian,
  N.exactRow,
  N.scaleEveryPoint,
  N.g12,
  N.dailyPe,
  N.medianRequired,
  N.demandingCount,
  N.pairwise,
  N.barScale,
];

add("evaluate GRMN at the default levers", "evaluate", ev(GRMN), evalNotes);
add(
  "evaluate GRMN with year-by-year growth",
  "evaluate",
  ev(noDaily(GRMN), {
    baseline: {
      mode: "custom",
      trailingMonths: 12,
      custom: { yearly: true, rate: 0.12, rates: [0.18, 0.12, 0.06] },
    },
  }),
  evalNotes,
);
add(
  "evaluate GRMN with a flat custom rate and today's P/E",
  "evaluate",
  ev(noDaily(GRMN), {
    exitPe: { mode: "today", trailingMonths: 36 },
    baseline: {
      mode: "custom",
      trailingMonths: 12,
      custom: { yearly: false, rate: 0.1, rates: [] },
    },
  }),
  evalNotes,
);
add(
  "evaluate GRMN with the typical P/E and trailing growth",
  "evaluate",
  ev(noDaily(GRMN), {
    exitPe: { mode: "trailing", trailingMonths: 24 },
    baseline: { mode: "trailing", trailingMonths: 36 },
  }),
  evalNotes,
);
add(
  "evaluate GRMN with a custom exit P/E and today's growth",
  "evaluate",
  ev(noDaily(GRMN), {
    exitPe: { mode: "custom", custom: 18, trailingMonths: 36 },
    baseline: { mode: "today", trailingMonths: 12 },
    holdingYears: 5,
    dividend: 3.6,
  }),
  evalNotes,
);
add(
  "evaluate GRMN when dividends reach the target",
  "evaluate",
  ev(noDaily(GRMN), { price: 50, dividend: 30 }),
  [...evalNotes, N.dividendsReachTarget],
);
add("evaluate GRMN with negative EPS", "evaluate", ev(noDaily(GRMN), { ttmEps: -2 }), evalNotes);
add(
  "evaluate GRMN with user cases over one year",
  "evaluate",
  ev(noDaily(GRMN), {
    holdingYears: 1,
    chartRange: "1Y",
    scenarios: [
      { name: "Bear", growth: 0.3, exitPe: 30 },
      { name: "Base", growth: 0.05, exitPe: 15 },
      { name: "Bull", growth: 0.1, exitPe: 18 },
    ],
  }),
  evalNotes,
);
add(
  "evaluate GRMN over ten years with a 6-month chart",
  "evaluate",
  ev(GRMN, { holdingYears: 10, chartRange: "6M" }),
  evalNotes,
);
add("evaluate a slowing company", "evaluate", ev(noDaily(NKE)), evalNotes);
add("evaluate a slow grower", "evaluate", ev(noDaily(KO)), evalNotes);
add("evaluate a fast grower", "evaluate", ev(noDaily(PLTR)), evalNotes);
add("evaluate a company with dilution", "evaluate", ev(noDaily(AMD)), evalNotes);
add(
  "evaluate a short history",
  "evaluate",
  { history: { quarters: short(gq, 6), daily: [] }, levers: defaultLevers(GRMN) },
  [...evalNotes, N.trailingHistory, N.typicalHistory],
);
add(
  "evaluate today's P/E that rounds to zero",
  "evaluate",
  ev(noDaily(GRMN), { price: 1, ttmEps: 10, exitPe: { mode: "today", trailingMonths: 36 } }),
  [...evalNotes, N.exitPePositive],
);
add(
  "evaluate user cases with a zero and a negative exit P/E",
  "evaluate",
  ev(noDaily(GRMN), {
    scenarios: [
      { name: "Bear", growth: 0.05, exitPe: 0 },
      { name: "Base", growth: 0.1, exitPe: -5 },
      { name: "Bull", growth: 0.2, exitPe: 25 },
    ],
  }),
  [...evalNotes, N.scenarioRange],
);
add(
  "evaluate a collapse in the latest quarter",
  "evaluate",
  { history: { quarters: withEps(short(gq, 5), 4, -4), daily: [] }, levers: defaultLevers(GRMN) },
  [...evalNotes, N.scenarioRange],
);
const cancel = [0, 1.0001e-4, -1e-4, 0, 0, 1];
add(
  "evaluate a TTM EPS that cancels to near zero",
  "evaluate",
  {
    history: {
      quarters: short(gq, 6).map((q, i) => ({
        ...q,
        eps: cancel[i],
        close: i === 3 || i === 4 ? 1e8 : null,
      })),
      daily: [],
    },
    levers: defaultLevers(GRMN, {
      price: 100,
      ttmEps: 4,
      holdingYears: 1,
      exitPe: { mode: "trailing", trailingMonths: 36 },
    }),
  },
  [...evalNotes, N.requiredEndpoints],
);
// perShareTimes multiplies every price, close and EPS of a profile by k, so
// that its P/Es and growth rates stay the same.
const perShareTimes = (p, k) => {
  const times = (v) => (v == null ? v : v * k);
  return {
    ...p,
    price: p.price * k,
    ttmEps: p.ttmEps * k,
    history: {
      quarters: p.history.quarters.map((q) => ({ ...q, eps: times(q.eps), close: times(q.close) })),
      daily: p.history.daily.map((d) => ({ ...d, close: times(d.close), ttmEps: times(d.ttmEps) })),
    },
  };
};
const lastDays = (p, k) => ({ ...p, history: { ...p.history, daily: p.history.daily.slice(-k) } });
add(
  "evaluate a share price of 1,500,000",
  "evaluate",
  ev(lastDays(perShareTimes(GRMN, 1.5e6 / GRMN.price), 26), { price: 1.5e6, chartRange: "6M" }),
  [...evalNotes, N.inputBounds],
);
// The edges of the price bound, at a close a little above GRMN's.
const atPrice = (price, closes) => ({
  history: {
    quarters: gq.map((q, i) => (i === gq.length - 1 ? { ...q, close: closes.quarter } : q)),
    daily: [{ date: "2026-01-02", close: closes.daily, ttmEps: GRMN.ttmEps }],
  },
  levers: defaultLevers(GRMN, { price }),
});
const edge = 1e8;
const above = 1e8 + 1;
for (const [name, input, accepted] of [
  ["accepts a price and closes of 10^8", atPrice(edge, { quarter: edge, daily: edge }), true],
  ["rejects a price above 10^8", atPrice(above, { quarter: 300, daily: 300 }), false],
  ["rejects a quarter-end close above 10^8", atPrice(300, { quarter: above, daily: 300 }), false],
  ["rejects a daily close above 10^8", atPrice(300, { quarter: 300, daily: above }), false],
  [
    "rejects a TTM EPS above 10^6",
    {
      ...atPrice(300, { quarter: 300, daily: 300 }),
      levers: defaultLevers(GRMN, { ttmEps: 1e6 + 1 }),
    },
    false,
  ],
  [
    "rejects a dividend above 10^6",
    {
      ...atPrice(300, { quarter: 300, daily: 300 }),
      levers: defaultLevers(GRMN, { dividend: 1e6 + 1 }),
    },
    false,
  ],
])
  hand(name, "evaluateAccepts", input, accepted, 0, N.inputBounds);
add(
  "evaluate a baseline exactly 1.5 pp above the requirement",
  "evaluate",
  ev(noDaily(GRMN), {
    price: 100,
    ttmEps: 5,
    holdingYears: 1,
    exitPe: { mode: "custom", custom: 20, trailingMonths: 36 },
    baseline: {
      mode: "custom",
      trailingMonths: 12,
      custom: { yearly: false, rate: 0.165, rates: [] },
    },
  }),
  [...evalNotes, N.toneTolerance],
);
add(
  "evaluate a baseline that lands exactly on the target",
  "evaluate",
  ev(noDaily(GRMN), {
    price: 100,
    ttmEps: 5,
    holdingYears: 1,
    exitPe: { mode: "custom", custom: 20, trailingMonths: 36 },
    baseline: {
      mode: "custom",
      trailingMonths: 12,
      custom: { yearly: false, rate: 0.15, rates: [] },
    },
  }),
  [...evalNotes, N.targetTolerance],
);
add(
  "evaluate no history",
  "evaluate",
  { history: { quarters: [], daily: [] }, levers: defaultLevers(GRMN) },
  evalNotes,
);
add(
  "evaluate no history with the typical P/E",
  "evaluate",
  {
    history: { quarters: [], daily: [] },
    levers: defaultLevers(GRMN, { exitPe: { mode: "trailing", trailingMonths: 36 } }),
  },
  [...evalNotes, N.typicalToday],
);
add(
  "evaluate the lowest custom exit P/E",
  "evaluate",
  ev(noDaily(GRMN), {
    price: 1,
    ttmEps: 10,
    exitPe: { mode: "custom", custom: 0.5, trailingMonths: 36 },
  }),
  [...evalNotes, N.lowerExitPe],
);

// ------------------------------------------------------------------ write

// Every function of the engine has at least one case.
const fns = new Set(cases.map((c) => c.fn));
for (const fn of [
  "requiredGrowth",
  "requirement",
  "project",
  "exitPeNeeded",
  "tone",
  "verdict",
  "niceStep",
  "quarterSeries",
  "weightedTrend",
  "trailingGrowth",
  "trajectoryFill",
  "yearRates",
  "yearlyAverage",
  "baselinePath",
  "growthScale",
  "typicalPe",
  "estimatedExitPe",
  "returnGrid",
  "earningsPaths",
  "howDemanding",
  "defaultScenarios",
  "scenarios",
  "shareCountEvents",
  "correlation",
  "bandPrice",
  "peBands",
  "priceChart",
  "financials",
  "compareScale",
  "evaluate",
  "evaluateAccepts",
]) {
  assert.ok(fns.has(fn), `no case for ${fn}`);
}

// A case named for dividends that reach the target must reach it.
for (const c of cases.filter((c) => /dividends (alone |exactly )?reach the target/.test(c.name))) {
  const reason =
    { requirement: c.expected.growth?.reason, evaluate: c.expected.required?.growth.reason }[
      c.fn
    ] ?? c.expected.reason;
  if (c.fn !== "returnGrid" && c.fn !== "earningsPaths")
    assert.equal(reason, "dividends_reach_target", c.name);
}

mkdirSync(outDir, { recursive: true });
writeFileSync(join(outDir, "cases.json"), format({ version: 1, cases }) + "\n");

const exampleInput = { history: GRMN_FULL.history, levers: defaultLevers(GRMN_FULL) };
const example = { input: exampleInput, output: clean(O.evaluate(structuredClone(exampleInput))) };
writeFileSync(join(outDir, "evaluation-example.json"), format(example) + "\n");
console.log(`wrote ${cases.length} cases to ${outDir}`);
