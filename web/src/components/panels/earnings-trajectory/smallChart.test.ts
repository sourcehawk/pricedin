import { describe, expect, it } from "vitest";
import { linePath } from "./smallChart";

describe("linePath", () => {
  it("joins points into one path", () => {
    expect(
      linePath([
        [0, 1],
        [2, 3],
        [4, 5],
      ]),
    ).toBe("M0.0,1.0 L2.0,3.0 L4.0,5.0");
  });

  it("breaks the line at a missing point instead of bridging it", () => {
    expect(linePath([[0, 1], null, [4, 5], [6, 7]])).toBe("M0.0,1.0 M4.0,5.0 L6.0,7.0");
  });

  it("breaks the line at a point without a finite y", () => {
    expect(
      linePath([
        [0, 1],
        [2, Number.NaN],
        [4, 5],
      ]),
    ).toBe("M0.0,1.0 M4.0,5.0");
  });

  it("is empty without points", () => {
    expect(linePath([null, null])).toBe("");
  });
});
