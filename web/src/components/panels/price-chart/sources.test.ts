import { describe, expect, it } from "vitest";
import type { PriceSource, QuarterSources, Source } from "@/api/types";
import { historyFixture, priceSourced } from "@/api/testing/fixtures";
import type { ChartPoint } from "@/engine";
import { closeSourced, ttmSourced } from "./sources";

const quarters = historyFixture.quarters;

const day = (change: Partial<ChartPoint> = {}): ChartPoint => ({
  date: "2026-09-30",
  close: 270,
  ttmEps: 10.8,
  ttmQuarters: quarters.slice(-4).map((q) => q.label),
  pe: { value: 25 },
  priceChange12m: { value: null, reason: "insufficient_history" },
  epsGrowth12m: { value: null, reason: "insufficient_history" },
  ...change,
});

describe("closeSourced", () => {
  it("gives the latest close its source on its own day", () => {
    expect(closeSourced(priceSourced.asOf, priceSourced.value, [], priceSourced)).toBe(
      priceSourced,
    );
  });

  it("gives a quarter-end close the source of that quarter", () => {
    const src: PriceSource = {
      kind: "price",
      provider: "massive",
      symbol: "GRMN",
      closeDate: "2025-06-27",
      rawClose: 201,
      splitFactor: 1,
    };
    const qs: QuarterSources[] = [{ ...(quarters[0] as QuarterSources), close: src }];
    expect(closeSourced("2025-06-27", 201, qs, null)).toEqual({
      value: 201,
      source: src,
      asOf: "2025-06-27",
      asOfLabel: "as of close",
    });
  });

  it("has no source for a day that neither the profile nor a quarter sources", () => {
    expect(closeSourced("2025-01-15", 180, quarters, priceSourced)).toBeNull();
  });

  it("has no source when the latest close of the profile differs", () => {
    expect(closeSourced(priceSourced.asOf, priceSourced.value + 1, [], priceSourced)).toBeNull();
  });
});

describe("ttmSourced", () => {
  it("adds up the sources of the four quarters that the engine names", () => {
    const s = ttmSourced(day(), quarters);
    const last4 = quarters.slice(-4);
    expect(s?.value).toBe(10.8);
    expect(s?.source).toEqual({
      kind: "derived",
      method: "ttm",
      estimate: false,
      inputs: last4.map((q) => q.eps),
    });
    // The example's quarters are filed on their last day, so the newest filing is the last quarter's.
    expect(s?.asOf).toBe(last4.at(-1)?.periodEnd);
  });

  it("is as of the newest filing among the quarters, such as a restatement", () => {
    const restated = quarters.map((q, i) =>
      i === quarters.length - 3 && q.eps?.kind === "sec"
        ? { ...q, eps: { ...q.eps, form: "10-Q/A", filed: "2026-10-15" } }
        : q,
    );
    expect(ttmSourced(day(), restated)?.asOf).toBe("2026-10-15");
  });

  it("is an estimate when one of its quarters is", () => {
    const estimated = quarters.map((q, i): QuarterSources => {
      if (i !== quarters.length - 1 || q.eps === null) {
        return q;
      }
      const eps: Source = {
        kind: "derived",
        method: "weighted_average_q4",
        estimate: true,
        inputs: [q.eps],
      };
      return { ...q, eps };
    });
    expect(ttmSourced(day(), estimated)?.source).toMatchObject({ estimate: true });
  });

  it("is null without a TTM EPS or named quarters", () => {
    expect(ttmSourced(day({ ttmEps: null }), quarters)).toBeNull();
    expect(ttmSourced(day({ ttmQuarters: [] }), quarters)).toBeNull();
  });

  it("is null when a named quarter has no source", () => {
    const qs = quarters.map((q, i) => (i === quarters.length - 1 ? { ...q, eps: null } : q));
    expect(ttmSourced(day(), qs)).toBeNull();
  });
});
