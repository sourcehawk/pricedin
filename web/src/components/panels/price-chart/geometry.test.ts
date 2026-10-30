import { describe, expect, it } from "vitest";
import {
  bandSegments,
  bottom,
  dayLabel,
  dayTime,
  labelSlots,
  left,
  seriesPath,
  monthTicks,
  nearestIndex,
  pointsPath,
  priceTick,
  shadePath,
  top,
  xScale,
  yScale,
} from "./geometry";

const id = (v: number) => v;

describe("seriesPath", () => {
  it("joins the values and breaks the line at a missing value", () => {
    expect(seriesPath([0, 1, 2, 3], [5, null, 6, 7], id, false)).toBe("M0.0,5.0M2.0,6.0L3.0,7.0");
  });

  it("draws a step on the day a stepped value changes", () => {
    expect(seriesPath([0, 1, 2], [5, 5, 8], id, true)).toBe("M0.0,5.0L1.0,5.0L2.0,5.0L2.0,8.0");
  });
});

describe("bandSegments", () => {
  it("steps the band on each day the TTM EPS changes", () => {
    expect(bandSegments([0, 1, 2], [2, 2, 3], 10, id)).toEqual([
      [
        [0, 20],
        [1, 20],
        [2, 20],
        [2, 30],
      ],
    ]);
  });

  it("ends a run on a day without a positive TTM EPS", () => {
    expect(bandSegments([0, 1, 2, 3], [2, -1, null, 3], 10, id)).toEqual([[[0, 20]], [[3, 30]]]);
  });

  it("has no runs without a positive TTM EPS", () => {
    expect(bandSegments([0, 1], [0, null], 10, id)).toEqual([]);
  });
});

describe("shadePath", () => {
  it("closes the area between two bands run by run", () => {
    const upper = [
      [
        [0, 1],
        [1, 1],
      ] as [number, number][],
    ];
    const lower = [
      [
        [0, 5],
        [1, 5],
      ] as [number, number][],
    ];
    expect(shadePath(upper, lower)).toBe("M0.0,1.0L1.0,1.0L1.0,5.0L0.0,5.0Z");
    expect(pointsPath(upper)).toBe("M0.0,1.0L1.0,1.0");
  });
});

describe("scales", () => {
  it("maps the range onto the plot, min at the bottom", () => {
    const y = yScale({ min: 0, max: 10 });
    expect(y(0)).toBe(bottom);
    expect(y(10)).toBe(top);
  });

  it("puts a zero-width range in the middle instead of dividing by zero", () => {
    expect(yScale({ min: 4, max: 4 })(4)).toBe((top + bottom) / 2);
  });

  it("puts a single day at the left edge", () => {
    expect(xScale(5, 5, 700)(5)).toBe(left);
  });
});

describe("monthTicks", () => {
  const t = (d: string) => dayTime(d);

  it("labels every sixth month over 3Y", () => {
    expect(monthTicks(t("2023-09-30"), t("2026-09-30"), "3Y").map((k) => k.label)).toEqual([
      "Jan '24",
      "Jul '24",
      "Jan '25",
      "Jul '25",
      "Jan '26",
      "Jul '26",
    ]);
  });

  it("labels every month over 6M", () => {
    expect(monthTicks(t("2026-03-30"), t("2026-06-30"), "6M").map((k) => k.label)).toEqual([
      "Apr '26",
      "May '26",
      "Jun '26",
    ]);
  });
});

describe("labelSlots", () => {
  it("keeps labels 15 apart from the bottom up", () => {
    expect(
      labelSlots([
        { multiple: 20, y: 100 },
        { multiple: 25, y: 95 },
      ]),
    ).toEqual([
      { multiple: 20, y: 100 },
      { multiple: 25, y: 85 },
    ]);
  });

  it("leaves out labels outside the plot", () => {
    expect(labelSlots([{ multiple: 50, y: top }]).length).toBe(0);
  });
});

describe("nearestIndex", () => {
  it("finds the nearest x", () => {
    expect(nearestIndex([0, 10, 20], 14)).toBe(1);
    expect(nearestIndex([0, 10, 20], 16)).toBe(2);
    expect(nearestIndex([0, 10, 20], -5)).toBe(0);
    expect(nearestIndex([5], 100)).toBe(0);
  });
});

describe("formats", () => {
  it("shows a day and a price tick", () => {
    expect(dayLabel("2026-09-30")).toBe("30 Sep 2026");
    expect(priceTick(1250)).toBe("$1,250");
    expect(priceTick(0.1 + 0.2)).toBe("$0.3");
    expect(priceTick(12_500)).toBe("$12.5k");
    expect(priceTick(100_000)).toBe("$100k");
  });
});
