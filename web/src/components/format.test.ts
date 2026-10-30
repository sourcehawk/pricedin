import { describe, expect, it } from "vitest";
import { pct, pp, signedPct, times, usd, years } from "./format";

describe("pct", () => {
  it("shows a fraction as a percent with one decimal", () => {
    expect(pct(0.148)).toBe("14.8%");
  });
  it("takes the number of decimals", () => {
    expect(pct(0.15, 0)).toBe("15%");
  });
  it("shows n/a for a missing or infinite value", () => {
    expect(pct(null)).toBe("n/a");
    expect(pct(Number.NaN)).toBe("n/a");
    expect(pct(Number.POSITIVE_INFINITY)).toBe("n/a");
  });
});

describe("signedPct", () => {
  it("puts a plus sign before a gain", () => {
    expect(signedPct(0.073)).toBe("+7.3%");
  });
  it("keeps the minus sign of a loss", () => {
    expect(signedPct(-0.2, 0)).toBe("-20%");
  });
  it("shows a value that rounds to zero without a sign", () => {
    expect(signedPct(-0.0001)).toBe("0.0%");
  });
});

describe("pp", () => {
  it("shows a difference of fractions in signed percentage points", () => {
    expect(pp(-0.091)).toBe("-9.1 pp");
    expect(pp(0.015)).toBe("+1.5 pp");
  });
  it("shows n/a for a missing value", () => {
    expect(pp(null)).toBe("n/a");
  });
});

describe("usd", () => {
  it("shows dollars with two decimals and thousands separators", () => {
    expect(usd(1234.5)).toBe("$1,234.50");
  });
  it("puts the minus sign before the dollar sign", () => {
    expect(usd(-3.2)).toBe("-$3.20");
  });
  it("shows n/a for a missing value", () => {
    expect(usd(undefined)).toBe("n/a");
  });
});

describe("times", () => {
  it("shows a multiple with one decimal and the times sign", () => {
    expect(times(25)).toBe("25.0×");
  });
  it("shows n/a for a missing value", () => {
    expect(times(null)).toBe("n/a");
  });
});

describe("years", () => {
  it("uses the singular for one year", () => {
    expect(years(1)).toBe("1 year");
    expect(years(3)).toBe("3 years");
  });
});
