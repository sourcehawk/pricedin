import { describe, expect, it } from "vitest";
import { exampleEvaluation, exampleHistory, exampleLevers } from "@/api/testing/fixtures";
import { evaluate, type Levers } from "@/engine";
import { baselineName, baselineNote, exitPeNote, trajectoryText, typicalText } from "./notes";

function ev(change: Partial<Levers> = {}) {
  return evaluate(exampleHistory, { ...exampleLevers, ...change });
}

describe("exitPeNote", () => {
  it("explains the fade of the estimated exit P/E with the trend and the growth at exit", () => {
    const e = exampleEvaluation;
    expect(e.exitPe.inUse.value?.rule).toBe("fade");
    const note = exitPeNote(e);
    expect(note).toContain("Today the market pays 25.0× earnings.");
    expect(note).toMatch(/If growth slows from \d+\.\d% now to about \d+\.\d% when you sell/);
    expect(note).toContain(`the estimate is ${String(e.exitPe.inUse.value?.pe.toFixed(1))}×.`);
  });

  it("keeps a low P/E", () => {
    const note = exitPeNote(ev({ price: 120 }));
    expect(note).toBe(
      "Today the market pays only 11.1× earnings, so the estimate keeps it rather than going lower.",
    );
  });

  it("names today's P/E in the today mode", () => {
    expect(exitPeNote(ev({ exitPe: { mode: "today" } }))).toBe(
      "Assumes the market still pays today's 25.0× earnings when you sell.",
    );
  });

  it("names the typical P/E and its months in the trailing mode", () => {
    expect(exitPeNote(ev({ exitPe: { mode: "trailing", trailingMonths: 24 } }))).toMatch(
      /^Uses the stock's typical P\/E: the median of its quarter-end P\/Es over the last 24 months, \d+\.\d×\./,
    );
  });

  it("compares a custom exit P/E with the estimate and today's", () => {
    expect(exitPeNote(ev({ exitPe: { mode: "custom", custom: 30 } }))).toMatch(
      /^Your own number\. For comparison, the estimate is \d+\.\d× and today's P\/E is 25\.0×\.$/,
    );
  });

  it("says that the model needs positive EPS", () => {
    expect(exitPeNote(ev({ ttmEps: -1 }))).toBe(
      "A P/E needs positive EPS. Loss-making companies need a different method.",
    );
  });
});

describe("typicalText", () => {
  it("gives the typical P/E to 0.5 with its months", () => {
    expect(typicalText(exampleEvaluation)).toMatch(
      /^typical \d+\.[05]× \(the median of the last 36 months\)$/,
    );
  });
});

describe("baselineNote", () => {
  it("describes the weighted trend", () => {
    expect(baselineNote(exampleEvaluation, exampleLevers)).toMatch(
      /^Year-over-year EPS growth of the last 8 quarters/,
    );
  });

  it("gives the TTM EPS range of trailing growth and the rounding of the months", () => {
    const l: Levers = { ...exampleLevers, baseline: { mode: "trailing", trailingMonths: 13 } };
    const note = baselineNote(evaluate(exampleHistory, l), l);
    expect(note).toMatch(
      /^Average yearly growth of TTM EPS over the last 12 months: from \$\d+\.\d\d to \$10\.80\./,
    );
    expect(note).toContain("Rounded to 12 months, because results come out every 3 months.");
  });

  it("describes year-by-year custom rates", () => {
    const l: Levers = {
      ...exampleLevers,
      baseline: { mode: "custom", custom: { yearly: true, rate: 0.1, rates: [0.1, 0.08] } },
    };
    expect(baselineNote(evaluate(exampleHistory, l), l)).toMatch(
      /^Your own growth rate for each year/,
    );
  });
});

describe("baselineName", () => {
  it("names each baseline as the copy does", () => {
    expect(baselineName(exampleEvaluation)).toBe("weighted trend");
    expect(baselineName(ev({ baseline: { mode: "trailing", trailingMonths: 24 } }))).toBe(
      "trailing 24-month growth",
    );
  });
});

describe("trajectoryText", () => {
  it("gives last year's change of the weighted trend and the rates that continue it", () => {
    const t = exampleEvaluation.trajectory.value;
    expect(t).not.toBeNull();
    const text = trajectoryText(exampleEvaluation);
    expect(text).toMatch(
      /^Over the last year the weighted trend went from -?\d+\.\d% to -?\d+\.\d% \([+-]\d+\.\d pp\)\. If it keeps (slowing|speeding up), but less each year: /,
    );
    expect(text).toContain((t?.rates ?? []).map((r) => `${(r * 100).toFixed(1)}%`).join(", "));
  });
});
