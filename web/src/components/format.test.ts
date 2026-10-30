import { describe, expect, it } from "vitest";
import {
  coefficient,
  factor,
  money,
  multiple,
  pct,
  points,
  pp,
  shareCount,
  signedPct,
  signedUsd,
  times,
  usd,
  years,
} from "./format";

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
  it("shows an amount under one cent with two significant digits, not as $0.00", () => {
    expect(usd(0.0042)).toBe("$0.0042");
    expect(usd(-0.004)).toBe("-$0.004");
    expect(usd(0.00999)).toBe("$0.01");
  });
  it("shows zero as zero", () => {
    expect(usd(0)).toBe("$0.00");
  });
});

describe("signedUsd", () => {
  it("puts a plus sign before a gain and keeps the minus of a loss", () => {
    expect(signedUsd(1.2)).toBe("+$1.20");
    expect(signedUsd(-0.4)).toBe("-$0.40");
  });
  it("shows n/a for a missing value", () => {
    expect(signedUsd(null)).toBe("n/a");
  });
});

describe("points", () => {
  it("shows the size of a difference in percentage points, without a sign", () => {
    expect(points(-0.091)).toBe("9.1");
    expect(points(0.15, 0)).toBe("15");
  });
});

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
  it("shows millions and billions with four significant digits", () => {
    expect(shareCount(191_710_000)).toBe("191.7M");
    expect(shareCount(2_150_000_000)).toBe("2.150B");
    expect(shareCount(24_530_000_000)).toBe("24.53B");
  });
  it("never hides a change of 2% or more", () => {
    expect(shareCount(1_000_000)).toBe("1.000M");
    expect(shareCount(1_020_000)).toBe("1.020M");
    expect(shareCount(999_000_000)).not.toBe(shareCount(1_019_000_000));
  });
  it("moves to billions where millions would round to 1000", () => {
    expect(shareCount(999_960_000)).toBe("1.000B");
  });
  it("shows a count under a million in whole shares", () => {
    expect(shareCount(950_000)).toBe("950,000");
  });
  it("shows n/a for a missing value", () => {
    expect(shareCount(null)).toBe("n/a");
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

describe("factor", () => {
  it("shows a multiple as a number without a trailing .0", () => {
    expect(factor(20)).toBe("20");
    expect(factor(22.5)).toBe("22.5");
    expect(factor(13.499999999)).toBe("13.5");
  });
});

describe("multiple", () => {
  it("shows a multiple without a trailing .0", () => {
    expect(multiple(20)).toBe("20×");
    expect(multiple(22.5)).toBe("22.5×");
  });
});

describe("coefficient", () => {
  it("shows a correlation with two decimals", () => {
    expect(coefficient(0.6234)).toBe("0.62");
    expect(coefficient(-0.31)).toBe("-0.31");
  });
  it("shows a value that rounds to zero without a sign", () => {
    expect(coefficient(-0.001)).toBe("0.00");
  });
  it("shows n/a for a missing value", () => {
    expect(coefficient(null)).toBe("n/a");
  });
});

describe("years", () => {
  it("uses the singular for one year", () => {
    expect(years(1)).toBe("1 year");
    expect(years(3)).toBe("3 years");
  });
});
