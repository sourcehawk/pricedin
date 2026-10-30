import { describe, expect, it } from "vitest";
import type { PriceFeed, QuarterSources, Source } from "@/api/types";
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
  const feed: PriceFeed = {
    provider: "massive",
    symbol: "GRMN",
    splits: [
      { date: "2024-03-01", ratio: 2 },
      { date: "2025-06-02", ratio: 3 },
    ],
  };

  it("gives the latest close the profile's source on its own day", () => {
    expect(closeSourced(priceSourced.asOf, priceSourced.value, feed, priceSourced)).toBe(
      priceSourced,
    );
  });

  it("gives a close after every split the provider's close of that day", () => {
    expect(closeSourced("2025-08-14", 201.5, feed, priceSourced)).toEqual({
      value: 201.5,
      source: {
        kind: "price",
        provider: "massive",
        symbol: "GRMN",
        closeDate: "2025-08-14",
        rawClose: 201.5,
        splitFactor: 1,
      },
      asOf: "2025-08-14",
      asOfLabel: "as of close",
    });
  });

  it("multiplies a close back by the ratio of every split after its day", () => {
    const before = closeSourced("2024-02-29", 50.25, feed, null);
    expect(before?.source).toMatchObject({ splitFactor: 6, rawClose: 301.5 });
    const between = closeSourced("2025-05-30", 80, feed, null);
    expect(between?.source).toMatchObject({ splitFactor: 3, rawClose: 240 });
  });

  it("does not adjust a close on the day of a split", () => {
    expect(closeSourced("2025-06-02", 81, feed, null)?.source).toMatchObject({
      splitFactor: 1,
      rawClose: 81,
    });
  });

  it("gives the latest day the provider's close when the profile's close differs", () => {
    const s = closeSourced(priceSourced.asOf, priceSourced.value + 1, feed, priceSourced);
    expect(s).not.toBe(priceSourced);
    expect(s).toMatchObject({ value: priceSourced.value + 1, asOf: priceSourced.asOf });
  });

  it("has no source without a price feed", () => {
    expect(closeSourced("2025-01-15", 180, null, priceSourced)).toBeNull();
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
