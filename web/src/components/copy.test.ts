import { describe, expect, it } from "vitest";
import { exampleEvaluation, exampleHistory, exampleLevers } from "@/api/testing/fixtures";
import { evaluate, type Levers } from "@/engine";
import { baselineLabel, baselineName, dividendsReach, joinNames, orDividends } from "./copy";

function ev(change: Partial<Levers> = {}) {
  return evaluate(exampleHistory, { ...exampleLevers, ...change });
}

const custom = ev({ baseline: { mode: "custom", custom: { yearly: false, rate: 0.1 } } });

describe("baselineName", () => {
  it("names each baseline as the copy does", () => {
    expect(baselineName(exampleEvaluation)).toBe("weighted trend");
    expect(baselineName(ev({ baseline: { mode: "trailing", trailingMonths: 24 } }))).toBe(
      "trailing 24-month growth",
    );
    expect(baselineName(custom)).toBe("your custom growth");
  });
});

describe("baselineLabel", () => {
  it("leaves out the custom baseline's own your, so a sentence can put one before it", () => {
    expect(baselineLabel(custom)).toBe("custom growth");
    expect(baselineLabel(exampleEvaluation)).toBe("weighted trend");
  });
});

describe("dividendsReach", () => {
  it("is true only when the dividends alone reach the target", () => {
    expect(dividendsReach(exampleEvaluation)).toBe(false);
    expect(dividendsReach(ev({ dividend: 150 }))).toBe(true);
  });
});

describe("orDividends", () => {
  it("shows the no-growth line when the dividends reach the target, and the line otherwise", () => {
    const line = orDividends("no growth", () => "line");
    expect(line(ev({ dividend: 150 }))).toBe("no growth");
    expect(line(exampleEvaluation)).toBe("line");
  });
});

describe("joinNames", () => {
  it("lists names as a sentence does", () => {
    expect(joinNames([])).toBe("");
    expect(joinNames(["Bear"])).toBe("Bear");
    expect(joinNames(["Bear", "Bull"])).toBe("Bear and Bull");
    expect(joinNames(["Bear", "Base", "Bull"])).toBe("Bear, Base and Bull");
  });
});
