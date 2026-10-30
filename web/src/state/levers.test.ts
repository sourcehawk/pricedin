import { describe, expect, it } from "vitest";
import {
  defaultLevers,
  engineLevers,
  leversReducer,
  type LeverAction,
  type LeverState,
} from "./levers";

function run(...actions: LeverAction[]): LeverState {
  return actions.reduce(leversReducer, defaultLevers);
}

/** edited is a state with every input changed from its default. */
const edited: LeverState = run(
  { type: "pickStock", ticker: "GRMN" },
  { type: "setStartingPoint", field: "price", value: 250 },
  { type: "setStartingPoint", field: "ttmEps", value: 9 },
  { type: "setStartingPoint", field: "dividend", value: 3 },
  { type: "setTargetReturn", value: 0.12 },
  { type: "setHoldingYears", value: 5 },
  { type: "setExitPeMode", mode: "custom", current: 21 },
  { type: "setExitPeMonths", value: 24 },
  { type: "setBaselineMonths", value: 24 },
  { type: "setGrowth", value: 0.08 },
  { type: "pinScenarios", cases: [{ name: "Base", growth: 0.1, exitPe: 20 }] },
  { type: "setChartRange", range: "1Y" },
  { type: "setView", key: "grid.view", value: "cum" },
);

describe("defaults", () => {
  it("start from the brief's defaults", () => {
    expect(defaultLevers.targetReturn).toBe(0.15);
    expect(defaultLevers.holdingYears).toBe(3);
    expect(defaultLevers.exitPe.mode).toBe("estimated");
    expect(defaultLevers.exitPe.trailingMonths).toBe(36);
    expect(defaultLevers.baseline.mode).toBe("weighted");
    expect(defaultLevers.baseline.trailingMonths).toBe(12);
    expect(defaultLevers.price).toBeNull();
    expect(defaultLevers.ttmEps).toBeNull();
    expect(defaultLevers.dividend).toBeNull();
    expect(defaultLevers.scenarios).toBeNull();
    expect(defaultLevers.chartRange).toBe("3Y");
  });
});

describe("pickStock", () => {
  it("resets every input except the target return and the holding period", () => {
    const s = leversReducer(edited, { type: "pickStock", ticker: "KO" });
    expect(s).toEqual({
      ...defaultLevers,
      ticker: "KO",
      targetReturn: 0.12,
      holdingYears: 5,
      chartRange: "1Y",
      views: { "grid.view": "cum" },
    });
  });

  it("keeps the chart range and the panel views, which are not inputs", () => {
    const s = leversReducer(edited, { type: "pickStock", ticker: "KO" });
    expect(s.chartRange).toBe("1Y");
    expect(s.views).toEqual({ "grid.view": "cum" });
  });

  it("unpins the scenarios so the next evaluation pins its defaults", () => {
    const s = leversReducer(edited, { type: "pickStock", ticker: "KO" });
    expect(s.scenarios).toBeNull();
  });

  it("stores the ticker in upper case", () => {
    expect(run({ type: "pickStock", ticker: "grmn" }).ticker).toBe("GRMN");
  });
});

describe("reset", () => {
  it("resets every input, the target return and holding period included, and keeps the stock", () => {
    const s = leversReducer(edited, { type: "reset" });
    expect(s).toEqual({ ...defaultLevers, ticker: "GRMN" });
  });
});

describe("scenario pinning", () => {
  const cases = [
    { name: "Bear", growth: 0.06, exitPe: 15 },
    { name: "Base", growth: 0.12, exitPe: 20 },
  ];

  it("pins the cases when none are pinned", () => {
    const s = run({ type: "pickStock", ticker: "GRMN" }, { type: "pinScenarios", cases });
    expect(s.scenarios).toEqual(cases);
  });

  it("keeps the pinned cases when a later evaluation offers other defaults", () => {
    const s = run(
      { type: "pinScenarios", cases },
      { type: "pinScenarios", cases: [{ name: "Base", growth: 0.2, exitPe: 30 }] },
    );
    expect(s.scenarios).toEqual(cases);
  });

  it("replaces the cases when the user edits them", () => {
    const mine = [{ name: "Base", growth: 0.2, exitPe: 30 }];
    const s = run({ type: "pinScenarios", cases }, { type: "setScenarios", cases: mine });
    expect(s.scenarios).toEqual(mine);
  });
});

describe("clamping to the engine's bounds", () => {
  it("limits the target return to 0 to 50%", () => {
    expect(run({ type: "setTargetReturn", value: 0.8 }).targetReturn).toBe(0.5);
    expect(run({ type: "setTargetReturn", value: -0.1 }).targetReturn).toBe(0);
  });

  it("rounds the holding period to whole years from 1 to 10", () => {
    expect(run({ type: "setHoldingYears", value: 12 }).holdingYears).toBe(10);
    expect(run({ type: "setHoldingYears", value: 0 }).holdingYears).toBe(1);
    expect(run({ type: "setHoldingYears", value: 4.6 }).holdingYears).toBe(5);
  });

  it("limits a custom exit P/E to 0.5 to 1000", () => {
    expect(run({ type: "setExitPe", value: 5000 }).exitPe.custom).toBe(1000);
    expect(run({ type: "setExitPe", value: 0.1 }).exitPe.custom).toBe(0.5);
  });

  it("limits custom growth to -50% to 200%", () => {
    expect(run({ type: "setGrowth", value: 3 }).baseline.rate).toBe(2);
    expect(run({ type: "setCustomRate", value: -0.9 }).baseline.rate).toBe(-0.5);
    const s = run({ type: "setYearRate", index: 1, value: 9, current: [0.1, 0.1, 0.1] });
    expect(s.baseline.rates).toEqual([0.1, 2, 0.1]);
  });

  it("rounds look-backs to whole months from 1 to 120", () => {
    expect(run({ type: "setExitPeMonths", value: 500 }).exitPe.trailingMonths).toBe(120);
    expect(run({ type: "setBaselineMonths", value: 0 }).baseline.trailingMonths).toBe(1);
    expect(run({ type: "setBaselineMonths", value: 13.4 }).baseline.trailingMonths).toBe(13);
  });

  it("ignores a value that is not a finite number", () => {
    expect(run({ type: "setTargetReturn", value: Number.NaN })).toEqual(defaultLevers);
    expect(run({ type: "setHoldingYears", value: Number.POSITIVE_INFINITY })).toEqual(
      defaultLevers,
    );
  });
});

describe("starting point", () => {
  it("keeps a share price of 10^8, the engine's largest", () => {
    expect(run({ type: "setStartingPoint", field: "price", value: 1e8 }).price).toBe(1e8);
  });

  it("clamps a share price above 10^8 to 10^8", () => {
    expect(run({ type: "setStartingPoint", field: "price", value: 1e8 + 1 }).price).toBe(1e8);
  });

  it("clamps a share price that is not positive to the smallest price", () => {
    expect(run({ type: "setStartingPoint", field: "price", value: 0 }).price).toBe(1e-4);
    expect(run({ type: "setStartingPoint", field: "price", value: -5 }).price).toBe(1e-4);
  });

  it("clamps TTM EPS to the engine's sizes and keeps its sign", () => {
    const eps = (value: number) => run({ type: "setStartingPoint", field: "ttmEps", value }).ttmEps;
    expect(eps(2e6)).toBe(1e6);
    expect(eps(-2e6)).toBe(-1e6);
    expect(eps(5e-5)).toBe(1e-4);
    expect(eps(-5e-5)).toBe(-1e-4);
  });

  it("clamps a dividend above 10^6 to 10^6", () => {
    expect(run({ type: "setStartingPoint", field: "dividend", value: 2e6 }).dividend).toBe(1e6);
  });

  it("accepts zero and negative EPS, which the engine reports as needing positive EPS", () => {
    expect(run({ type: "setStartingPoint", field: "ttmEps", value: -2 }).ttmEps).toBe(-2);
    expect(run({ type: "setStartingPoint", field: "ttmEps", value: 0 }).ttmEps).toBe(0);
  });

  it("treats a negative dividend as no dividend", () => {
    expect(run({ type: "setStartingPoint", field: "dividend", value: -1 }).dividend).toBe(0);
  });

  it("goes back to the stock's value on null", () => {
    const s = run(
      { type: "setStartingPoint", field: "price", value: 120 },
      { type: "setStartingPoint", field: "price", value: null },
    );
    expect(s.price).toBeNull();
  });
});

describe("growth baseline", () => {
  it("switches to a flat custom rate when the user types a growth rate", () => {
    const s = run({ type: "setYearly", yearly: true }, { type: "setGrowth", value: 0.09 });
    expect(s.baseline.mode).toBe("custom");
    expect(s.baseline.yearly).toBe(false);
    expect(s.baseline.rate).toBe(0.09);
  });

  it("follows the trajectory year by year", () => {
    const s = run({ type: "followTrajectory", rates: [0.12, 0.1, 0.09] });
    expect(s.baseline).toMatchObject({
      mode: "custom",
      yearly: true,
      rates: [0.12, 0.1, 0.09],
      ratesSource: "trajectory",
    });
  });

  it("keeps an earlier year edit when the next one comes with the same displayed rates", () => {
    const shown = [0.12, 0.1, 0.09];
    const s = run(
      { type: "followTrajectory", rates: shown },
      { type: "setYearRate", index: 0, value: 0.2, current: shown },
      { type: "setYearRate", index: 1, value: 0.15, current: shown },
    );
    expect(s.baseline.rates).toEqual([0.2, 0.15, 0.09]);
  });

  it("makes the year rates the user's own when one year is edited", () => {
    const s = run(
      { type: "followTrajectory", rates: [0.12, 0.1, 0.09] },
      { type: "setYearRate", index: 2, value: 0.05, current: [0.12, 0.1, 0.09] },
    );
    expect(s.baseline.rates).toEqual([0.12, 0.1, 0.05]);
    expect(s.baseline.ratesSource).toBe("user");
  });
});

describe("exit P/E", () => {
  it("starts a custom exit P/E at the exit P/E in use", () => {
    const s = run({ type: "setExitPeMode", mode: "custom", current: 18.5 });
    expect(s.exitPe).toMatchObject({ mode: "custom", custom: 18.5 });
  });

  it("switches to custom when the user types an exit P/E", () => {
    const s = run({ type: "setExitPe", value: 24 });
    expect(s.exitPe).toMatchObject({ mode: "custom", custom: 24 });
  });
});

describe("engineLevers", () => {
  const stock = { price: 270, ttmEps: 10.8, dividend: 1.2 };

  it("takes the starting point from the stock unless the user typed it", () => {
    const l = engineLevers(
      run({ type: "setStartingPoint", field: "ttmEps", value: 11 }),
      stock,
      null,
    );
    expect(l).toMatchObject({ price: 270, ttmEps: 11, dividend: 1.2 });
  });

  it("is null when the stock has no price or TTM EPS and the user typed none", () => {
    expect(engineLevers(defaultLevers, { price: null, ttmEps: 10, dividend: 0 }, null)).toBeNull();
    expect(engineLevers(defaultLevers, { price: 10, ttmEps: null, dividend: 0 }, null)).toBeNull();
  });

  it("sends no scenarios until they are pinned, so the engine returns its defaults", () => {
    expect(engineLevers(defaultLevers, stock, null)?.scenarios).toEqual([]);
  });

  it("extends trajectory rates to a longer holding period from the trajectory", () => {
    const s = run(
      { type: "followTrajectory", rates: [0.12, 0.1, 0.09] },
      { type: "setHoldingYears", value: 5 },
    );
    const l = engineLevers(s, stock, [0.12, 0.1, 0.09, 0.085, 0.08]);
    expect(l?.baseline.custom?.rates).toEqual([0.12, 0.1, 0.09, 0.085, 0.08]);
  });

  it("does not extend the user's own rates", () => {
    const s = run(
      { type: "setYearRate", index: 0, value: 0.2, current: [0.1, 0.1, 0.1] },
      { type: "setHoldingYears", value: 5 },
    );
    const l = engineLevers(s, stock, [0.12, 0.1, 0.09, 0.085, 0.08]);
    expect(l?.baseline.custom?.rates).toEqual([0.2, 0.1, 0.1]);
  });
});
