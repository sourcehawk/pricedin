import { afterEach, describe, expect, it, vi } from "vitest";
import { defaultLevers, leversReducer, type LeverState } from "./levers";
import { loadLevers, saveLevers, storageKey } from "./storage";

const state: LeverState = [
  { type: "pickStock", ticker: "KO" } as const,
  { type: "setTargetReturn", value: 0.1 } as const,
  { type: "followTrajectory", rates: [0.05, 0.04] } as const,
  { type: "setView", key: "grid.view", value: "px" } as const,
].reduce(leversReducer, defaultLevers);

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("loadLevers and saveLevers", () => {
  it("give back the saved state", () => {
    saveLevers(state);
    expect(loadLevers()).toEqual(state);
  });

  it("give null when nothing is saved", () => {
    expect(loadLevers()).toBeNull();
  });

  it("give null for a value that is not JSON", () => {
    localStorage.setItem(storageKey, "{not json");
    expect(loadLevers()).toBeNull();
  });

  it("give null for another version", () => {
    localStorage.setItem(storageKey, JSON.stringify({ version: 99, state }));
    expect(loadLevers()).toBeNull();
  });

  it("use the default for each field that has a wrong type", () => {
    localStorage.setItem(
      storageKey,
      JSON.stringify({
        version: 1,
        state: { ...state, targetReturn: "high", exitPe: { mode: "sideways" }, views: [] },
      }),
    );
    expect(loadLevers()).toEqual({
      ...state,
      targetReturn: defaultLevers.targetReturn,
      exitPe: defaultLevers.exitPe,
      views: {},
    });
  });

  it("clamp a stored value to the engine's bounds", () => {
    localStorage.setItem(
      storageKey,
      JSON.stringify({ version: 1, state: { ...state, holdingYears: 40 } }),
    );
    expect(loadLevers()?.holdingYears).toBe(10);
  });

  it("survive storage that throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(() => {
      saveLevers(state);
    }).not.toThrow();
    expect(loadLevers()).toBeNull();
  });
});
