// @vitest-environment node
import {
  evaluate,
  InvalidInputError,
  type BaselineMode,
  type ChartRange,
  type ExitPeMode,
  type Levers,
  type PricePoint,
  type Quarter,
} from "./index";
import { compareJson, readJson, toJson, type Json } from "./testing/json";

interface Example {
  input: { history: { quarters: Quarter[]; daily: PricePoint[] }; levers: Levers };
  output: Json;
}

const example = readJson("evaluation-example.json") as Example;
const { history, levers } = example.input;

/** copyHistory returns a history whose quarters and days can be changed without touching the example. */
function copyHistory(): { quarters: Quarter[]; daily: PricePoint[] } {
  return {
    quarters: history.quarters.map((q) => ({ ...q })),
    daily: history.daily.map((d) => ({ ...d })),
  };
}

function quarter(i: number, eps: number): Quarter {
  const month = String(((i % 4) + 1) * 3).padStart(2, "0");
  return {
    label: `Q${String((i % 4) + 1)} FY${String(20 + Math.floor(i / 4))}`,
    periodEnd: `${String(2020 + Math.floor(i / 4))}-${month}-28`,
    eps,
    close: 3,
    dilutedShares: 1e6,
    revenue: 1e8,
    operatingIncome: 1e7,
    operatingCosts: 9e7,
    freeCashFlow: 5e6,
  };
}

describe("evaluate", () => {
  it("reproduces the committed GRMN example", () => {
    const diffs = compareJson(example.output, toJson(evaluate(history, levers)), 1e-9);
    expect(diffs.slice(0, 10)).toEqual([]);
  });

  const invalid: [string, (l: Levers, h: ReturnType<typeof copyHistory>) => Levers][] = [
    ["zero price", (l) => ({ ...l, price: 0 })],
    ["NaN EPS", (l) => ({ ...l, ttmEps: NaN })],
    ["EPS too close to zero", (l) => ({ ...l, ttmEps: 1e-9 })],
    ["price above a million", (l) => ({ ...l, price: 2e6 })],
    ["negative dividend", (l) => ({ ...l, dividend: -1 })],
    ["dividend above a million", (l) => ({ ...l, dividend: 2e6 })],
    ["negative target", (l) => ({ ...l, targetReturn: -0.01 })],
    ["target above 50%", (l) => ({ ...l, targetReturn: 0.51 })],
    ["zero years", (l) => ({ ...l, holdingYears: 0 })],
    ["eleven years", (l) => ({ ...l, holdingYears: 11 })],
    ["a fraction of a year", (l) => ({ ...l, holdingYears: 2.5 })],
    ["a custom exit P/E without a value", (l) => ({ ...l, exitPe: { mode: "custom" } })],
    ["custom exit P/E below 0.5", (l) => ({ ...l, exitPe: { mode: "custom", custom: 0.4 } })],
    ["custom exit P/E above 1000", (l) => ({ ...l, exitPe: { mode: "custom", custom: 1001 } })],
    ["infinite custom exit P/E", (l) => ({ ...l, exitPe: { mode: "custom", custom: Infinity } })],
    ["unknown exit P/E mode", (l) => ({ ...l, exitPe: { mode: "forward" as ExitPeMode } })],
    ["negative look-back", (l) => ({ ...l, exitPe: { mode: "estimated", trailingMonths: -3 } })],
    [
      "look-back above 120 months",
      (l) => ({ ...l, baseline: { mode: "weighted", trailingMonths: 121 } }),
    ],
    [
      "a fraction of a month",
      (l) => ({ ...l, baseline: { mode: "weighted", trailingMonths: 12.5 } }),
    ],
    ["unknown baseline mode", (l) => ({ ...l, baseline: { mode: "analyst" as BaselineMode } })],
    [
      "custom rate above 200%",
      (l) => ({
        ...l,
        baseline: { mode: "custom", custom: { yearly: false, rate: 2.01, rates: null } },
      }),
    ],
    [
      "yearly rate below -50%",
      (l) => ({
        ...l,
        baseline: { mode: "custom", custom: { yearly: true, rate: 0, rates: [0.1, -0.51] } },
      }),
    ],
    [
      "case growth above 10^30",
      (l) => ({ ...l, scenarios: [{ name: "Bull", growth: 2e30, exitPe: 10 }] }),
    ],
    [
      "case exit P/E above 10^30",
      (l) => ({ ...l, scenarios: [{ name: "Bull", growth: 0.1, exitPe: 2e30 }] }),
    ],
    ["unknown chart range", (l) => ({ ...l, chartRange: "5Y" as ChartRange })],
    [
      "NaN quarter EPS",
      (l, h) => {
        h.quarters[3] = { ...quarter(3, 1), eps: NaN };
        return l;
      },
    ],
    [
      "revenue above 10^15",
      (l, h) => {
        h.quarters[18] = { ...quarter(18, 1), revenue: 2e15 };
        return l;
      },
    ],
    [
      "a period end that is not a date",
      (l, h) => {
        h.quarters[0] = { ...quarter(0, 1), periodEnd: "2024-1-31" };
        return l;
      },
    ],
    [
      "a period end that does not exist",
      (l, h) => {
        h.quarters[0] = { ...quarter(0, 1), periodEnd: "2026-02-30" };
        return l;
      },
    ],
    [
      "a period end in month 13",
      (l, h) => {
        h.quarters[0] = { ...quarter(0, 1), periodEnd: "2026-13-01" };
        return l;
      },
    ],
    [
      "February 29 of a year that is not a leap year",
      (l, h) => {
        h.daily = [{ date: "2023-02-29", close: 10, ttmEps: 1 }];
        return l;
      },
    ],
    [
      "infinite daily EPS",
      (l, h) => {
        h.daily[4] = { date: h.daily[4]?.date ?? "", close: 10, ttmEps: -Infinity };
        return l;
      },
    ],
    [
      "days out of order",
      (l, h) => {
        h.daily[3] = { date: h.daily[2]?.date ?? "", close: 10, ttmEps: 1 };
        return l;
      },
    ],
    [
      "a zero close",
      (l, h) => {
        h.daily[5] = { date: h.daily[5]?.date ?? "", close: 0, ttmEps: 1 };
        return l;
      },
    ],
  ];

  it.each(invalid)("rejects %s with an InvalidInputError", (_, change) => {
    const h = copyHistory();
    const l = change({ ...levers }, h);
    expect(() => evaluate(h, l)).toThrow(InvalidInputError);
  });

  it("accepts zero and negative EPS and reports needs_positive_eps", () => {
    for (const ttmEps of [0, -2]) {
      const ev = evaluate(history, { ...levers, ttmEps });
      expect(ev.required.growth).toEqual({ value: null, reason: "needs_positive_eps" });
      expect(ev.returnGrid).toEqual({ value: null, reason: "needs_positive_eps" });
      expect(ev.baselines.weighted.value).not.toBeNull();
    }
  });

  it("accepts February 29 of a leap year", () => {
    const h = copyHistory();
    h.daily = [{ date: "2024-02-29", close: 10, ttmEps: 1 }];
    expect(() => evaluate(h, levers)).not.toThrow();
  });

  // The Gregorian calendar repeats every 400 years, so a history moved back
  // 2000 years must give the same evaluation with its dates moved back too.
  it("evaluates a history in the years 0 to 99 as Go does", () => {
    const back = (date: string): string =>
      `${String(Number(date.slice(0, 4)) - 2000).padStart(4, "0")}${date.slice(4)}`;
    const h = copyHistory();
    const old = {
      quarters: h.quarters.map((q) => ({ ...q, periodEnd: back(q.periodEnd) })),
      daily: h.daily.map((d) => ({ ...d, date: back(d.date) })),
    };
    const l: Levers = { ...levers, chartRange: "6M" };
    const want = JSON.stringify(evaluate(h, l)).replace(
      /"(\d{4}-\d{2}-\d{2})"/g,
      (_, d: string) => `"${back(d)}"`,
    );
    expect(JSON.parse(JSON.stringify(evaluate(old, l)))).toEqual(JSON.parse(want));
  });

  // Go marshals a nil slice of a History as null, so the API can send one.
  it("reads null quarters and days as an empty history", () => {
    const empty = evaluate({ quarters: [], daily: [] }, levers);
    expect(toJson(evaluate({ quarters: null, daily: null }, levers))).toEqual(toJson(empty));
    expect(toJson(evaluate({ quarters: history.quarters, daily: null }, levers))).toEqual(
      toJson(evaluate({ quarters: history.quarters, daily: [] }, levers)),
    );
  });

  it("treats missing optional levers as their defaults", () => {
    const explicit = evaluate(history, {
      ...levers,
      exitPe: { mode: "estimated", trailingMonths: 36 },
      baseline: { mode: "weighted", trailingMonths: 12 },
      scenarios: [],
      chartRange: "3Y",
    });
    const implicit = evaluate(history, {
      ...levers,
      exitPe: { mode: "estimated" },
      baseline: { mode: "weighted" },
      scenarios: null,
      chartRange: "",
    });
    expect(toJson(implicit)).toEqual(toJson(explicit));
  });
});

/** mulberry32 is a small seeded generator, so a failing run can be repeated. */
function mulberry32(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

describe("evaluate at the edges of the input bounds", () => {
  it("never puts NaN or Infinity in the output, also with the pinned default cases", () => {
    const rnd = mulberry32(21);
    const int = (n: number): number => Math.floor(rnd() * n);
    const pick = <T>(...vs: T[]): T => vs[int(vs.length)] as T;
    const perShare = (): number =>
      pick(0, 1e-4, -1e-4, 1.0001e-4, 1e6, -1e6, 999999.9999, 0.5, -0.5);
    for (let run = 0; run < 300; run++) {
      const quarters = Array.from({ length: 2 + int(20) }, (_, i): Quarter => ({
        ...quarter(i, perShare()),
        close: pick(1e-4, 1e6, 3),
        dilutedShares: pick(0, 1e-6, 1e15),
        revenue: pick(0, 1e-6, -1e-6, 1e15, -1e15),
        operatingIncome: pick(1e-6, -1e15, 1e15),
        operatingCosts: pick(0, 1e15),
        freeCashFlow: pick(-1e15, 1e-6),
      }));
      const start = Date.UTC(2023, 0, 1);
      const daily = Array.from({ length: 30 + int(500) }, (_, i): PricePoint => ({
        date: new Date(start + i * 86400000).toISOString().slice(0, 10),
        close: pick(1e-4, 1e6, 7),
        ttmEps: perShare(),
      }));
      const l: Levers = {
        price: pick(1e-4, 1e6, 1),
        ttmEps: perShare(),
        dividend: pick(0, 1e6),
        targetReturn: pick(0, 0.5),
        holdingYears: 1 + int(10),
        exitPe: {
          mode: pick<ExitPeMode>("estimated", "today", "trailing", "custom"),
          custom: pick(0.5, 1000),
          trailingMonths: int(121),
        },
        baseline: {
          mode: pick<BaselineMode>("weighted", "today", "trailing", "custom"),
          trailingMonths: int(121),
          custom: {
            yearly: int(2) === 0,
            rate: pick(-0.5, 2),
            rates: [pick(-0.5, 2), pick(-0.5, 2)],
          },
        },
        scenarios:
          int(2) === 0
            ? [{ name: "Edge", growth: pick(-0.999999, 10), exitPe: pick(-1e4, 1e4, 0) }]
            : null,
        chartRange: pick<ChartRange>("6M", "1Y", "3Y"),
      };
      const h = { quarters, daily };
      const ev = evaluate(h, l);
      expect(() => toJson(ev), `run ${String(run)}`).not.toThrow();
      // The UI pins the default cases and evaluates again with them.
      const pinned = ev.scenarios.cases.map(({ name, growth, exitPe }) => ({
        name,
        growth,
        exitPe,
      }));
      expect(
        () => toJson(evaluate(h, { ...l, scenarios: pinned })),
        `run ${String(run)} pinned`,
      ).not.toThrow();
    }
  });
});

describe("evaluate speed", () => {
  // The page evaluates on every lever change, so one evaluation must fit in
  // a 16 ms frame with room to spare. The bound is generous, and the fastest
  // run counts, so that a loaded CI machine does not fail it.
  it("evaluates the GRMN example in well under a frame", () => {
    for (let i = 0; i < 5; i++) {
      evaluate(history, levers);
    }
    let fastestMs = Infinity;
    for (let i = 0; i < 20; i++) {
      const t0 = performance.now();
      evaluate(history, levers);
      fastestMs = Math.min(fastestMs, performance.now() - t0);
    }
    expect(fastestMs).toBeLessThan(50);
  });
});
