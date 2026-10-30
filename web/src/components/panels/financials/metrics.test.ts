import { describe, expect, it } from "vitest";
import { metricValue } from "./metrics";

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
