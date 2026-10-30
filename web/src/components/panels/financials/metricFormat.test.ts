import { describe, expect, it } from "vitest";
import { metricValue, money, shareCount } from "./metricFormat";

describe("money", () => {
  it("shows billions with the given decimals", () => {
    expect(money(1_990_000_000, 1)).toBe("$2.0B");
    expect(money(1_990_000_000, 2)).toBe("$1.99B");
  });

  it("shows millions as whole millions", () => {
    expect(money(612_400_000)).toBe("$612M");
  });

  it("shows smaller amounts as whole dollars", () => {
    expect(money(52_300)).toBe("$52,300");
  });

  it("puts the sign before the dollar sign", () => {
    expect(money(-210_000_000)).toBe("-$210M");
    expect(money(-1_500_000_000, 2)).toBe("-$1.50B");
  });

  it("shows n/a for a missing value", () => {
    expect(money(null)).toBe("n/a");
  });
});

describe("shareCount", () => {
  it("shows millions with one decimal and billions with two", () => {
    expect(shareCount(192_000_000)).toBe("192.0M");
    expect(shareCount(2_150_000_000)).toBe("2.15B");
  });

  it("shows a small count in whole shares", () => {
    expect(shareCount(950_000)).toBe("950,000");
  });

  it("shows n/a for a missing value", () => {
    expect(shareCount(null)).toBe("n/a");
  });
});

describe("metricValue", () => {
  it("shows EPS in dollars and cents", () => {
    expect(metricValue("eps", 2.75)).toBe("$2.75");
  });

  it("shows diluted shares as a share count", () => {
    expect(metricValue("dilutedShares", 192_000_000)).toBe("192.0M");
  });

  it("shows the other metrics as dollar amounts", () => {
    expect(metricValue("freeCashFlow", 293_000_000)).toBe("$293M");
  });
});
