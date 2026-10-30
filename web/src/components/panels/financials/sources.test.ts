import { describe, expect, it } from "vitest";
import type { Source } from "@/api/types";
import { financials } from "@/engine";
import { filedOn, filingUrl, financialsHistory } from "./testing";
import { latestFiling, marginSources, metricSources } from "./sources";

const history = financialsHistory();
const view = financials(history.history.quarters);

describe("metricSources", () => {
  it("gives each quarter of the view its value with the source of that quarter", () => {
    const s = metricSources(view, history.quarters);
    const last = s.revenue.at(-1);
    expect(last?.value).toBe(1_990_000_000);
    expect(last?.source).toMatchObject({
      kind: "sec",
      periodEnd: "2026-09-30",
      concept: "Revenues",
    });
    expect(s.revenue).toHaveLength(view.quarters.length);
  });

  it("dates a value by the newest filing among its inputs", () => {
    const s = metricSources(view, history.quarters);
    expect(s.freeCashFlow.at(-1)?.asOf).toBe(filedOn("2026-09-30"));
    expect(s.eps[0]?.asOf).toBe(filedOn("2024-12-31"));
  });

  it("matches quarters by their period end, whatever the order of the sources", () => {
    const s = metricSources(view, [...history.quarters].reverse());
    expect(s.eps.at(-1)?.source).toMatchObject({ periodEnd: "2026-09-30" });
  });

  it("has no source for a value that the view lacks or that has no source", () => {
    const h = financialsHistory((qs) =>
      qs.map((q) => (q.periodEnd === "2026-06-30" ? { ...q, revenue: null } : q)),
    );
    const v = financials(h.history.quarters);
    const noSource = h.quarters.map((q) =>
      q.periodEnd === "2026-03-31" ? { ...q, freeCashFlow: null } : q,
    );
    const s = metricSources(v, noSource);
    expect(s.revenue.at(-2)).toBeNull();
    expect(s.freeCashFlow.at(-3)).toBeNull();
  });
});

describe("marginSources", () => {
  it("gives the operating margin with the filings of operating income and revenue as its inputs", () => {
    const m = marginSources(view, history.quarters).at(-1);
    expect(m?.value).toBeCloseTo(612 / 1990);
    expect(m?.source).toMatchObject({ kind: "derived", method: "derived" });
    const inputs = m?.source.kind === "derived" ? m.source.inputs : [];
    expect(inputs.map((s) => (s.kind === "sec" ? s.concept : s.kind))).toEqual([
      "OperatingIncomeLoss",
      "Revenues",
    ]);
  });

  it("lists the filings behind a derived operating income, not the derivation", () => {
    const q4 = history.quarters.map((q) => {
      if (q.periodEnd !== "2025-12-31" || q.operatingIncome === null) {
        return q;
      }
      const derived: Source = {
        kind: "derived",
        method: "fy_difference",
        estimate: false,
        inputs: [q.operatingIncome, q.operatingIncome],
      };
      return { ...q, operatingIncome: derived };
    });
    const m = marginSources(view, q4)[4];
    const inputs = m?.source.kind === "derived" ? m.source.inputs : [];
    expect(inputs.map((s) => s.kind)).toEqual(["sec", "sec", "sec"]);
  });
});

describe("latestFiling", () => {
  it("is the newest filing among the sources of the view, with its quarter", () => {
    const f = latestFiling(view, history.quarters);
    expect(f).toEqual({
      label: "Q3 FY26",
      form: "10-Q",
      filed: filedOn("2026-09-30"),
      url: filingUrl("0001121788-26-090001"),
    });
  });

  it("names the newer quarter when a filing also restates an older one", () => {
    const filed = filedOn("2026-09-30");
    const restated = history.quarters.map((q) =>
      q.periodEnd === "2025-09-30" && q.revenue?.kind === "sec"
        ? { ...q, revenue: { ...q.revenue, filed } }
        : q,
    );
    expect(latestFiling(view, restated)?.label).toBe("Q3 FY26");
  });

  it("is null without any filing", () => {
    expect(latestFiling(view, [])).toBeNull();
  });
});
