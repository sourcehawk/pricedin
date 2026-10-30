import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { historyFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, withApiServer } from "@/api/testing/server";
import { pct, signedPct, usd } from "@/components/format";
import type { Paths } from "@/engine";
import { useLeverDispatch } from "@/state/StockProvider";
import { renderReady } from "@/testing/render";
import { EarningsTrajectoryPanel } from "./EarningsTrajectoryPanel";
import { evaluationProbe, leversWith, WhenReady } from "./testing";

withApiServer();

/** RaiseEps changes the TTM EPS lever without moving the pointer. */
function RaiseEps() {
  const dispatch = useLeverDispatch();
  return (
    <button
      type="button"
      onClick={() => {
        dispatch({ type: "setStartingPoint", field: "ttmEps", value: 12 });
      }}
    >
      Raise EPS
    </button>
  );
}

function LeverButtons() {
  const dispatch = useLeverDispatch();
  return (
    <>
      <button
        type="button"
        onClick={() => {
          dispatch({ type: "setHoldingYears", value: 1 });
        }}
      >
        One year
      </button>
      <button
        type="button"
        onClick={() => {
          dispatch({ type: "setStartingPoint", field: "ttmEps", value: -1 });
        }}
      >
        Negative EPS
      </button>
    </>
  );
}

async function show(levers = leversWith()) {
  const probe = evaluationProbe();
  await renderReady(
    <>
      <probe.Probe />
      <div data-lever="exitPe" data-testid="lever-exitPe" />
      <div data-lever="baseline" data-testid="lever-baseline" />
      <RaiseEps />
      <LeverButtons />
      <WhenReady>
        <EarningsTrajectoryPanel />
      </WhenReady>
    </>,
    { levers },
  );
  const ev = probe.current();
  if (ev.earningsPaths.value === null) {
    throw new Error("the example has a trajectory");
  }
  const paths: Paths = ev.earningsPaths.value;
  return { ev, paths, chart: screen.getByRole("img", { name: /TTM EPS/ }) };
}

function columns(chart: Element): SVGRectElement[] {
  return [...chart.querySelectorAll<SVGRectElement>("rect.vcol")];
}

function guide(chart: Element): SVGLineElement {
  const l = chart.querySelector<SVGLineElement>("line.vx");
  if (l === null) {
    throw new Error("no guide line");
  }
  return l;
}

describe("EarningsTrajectoryPanel", () => {
  it("draws the TTM EPS history, the baseline path and the required path", async () => {
    const { chart } = await show();
    for (const key of ["hist", "base", "req"]) {
      expect(chart.querySelector(`path[data-walk~="${key}"]`)).not.toBeNull();
    }
    expect(screen.getByText("Reported TTM EPS")).toBeInTheDocument();
    expect(screen.getByText("Baseline continues")).toBeInTheDocument();
    expect(screen.getByText("Required path")).toBeInTheDocument();
  });

  it("labels the end of each path with the EPS at the exit", async () => {
    const { chart, paths } = await show();
    const end = paths.future.at(-1);
    expect(within(chart).getByText(usd(end?.baseline.value))).toBeInTheDocument();
    expect(within(chart).getByText(usd(end?.required.value))).toBeInTheDocument();
  });

  it("summarizes the chart for screen readers", async () => {
    const { chart, paths } = await show();
    const end = paths.future.at(-1);
    expect(chart).toHaveAccessibleName(
      expect.stringContaining(`required path reaches ${usd(end?.required.value)}`),
    );
  });

  it("shades the gap as bad when the price needs more growth than the baseline", async () => {
    const { chart, paths } = await show();
    expect(paths.gapTone.value).toBe("bad");
    expect(chart.querySelector('[data-walk~="shade"]')).toHaveClass("bad");
  });

  it("shades the gap as good when the baseline beats the required path", async () => {
    const { chart, paths } = await show(leversWith({ type: "setTargetReturn", value: 0 }));
    expect(paths.gapTone.value).toBe("good");
    expect(chart.querySelector('[data-walk~="shade"]')).toHaveClass("good");
  });

  it("has one column per reported quarter and per projected quarter", async () => {
    const { chart, paths } = await show();
    expect(columns(chart)).toHaveLength(paths.history.length + paths.future.length - 1);
  });

  it("snaps the tooltip to a reported quarter and draws the guide line there", async () => {
    const { chart, paths } = await show();
    const col = columns(chart)[2];
    if (col === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(col);
    const tip = screen.getByRole("tooltip");
    const q = paths.history[2];
    expect(tip).toHaveTextContent(q?.label ?? "");
    expect(tip).toHaveTextContent(`Reported TTM EPS${usd(q?.ttmEps.value)}`);
    expect(guide(chart)).toHaveAttribute("visibility", "visible");
    expect(guide(chart).getAttribute("x1")).toBe(col.dataset.x);
  });

  it("shows the baseline, the required EPS and the gap for a projected quarter", async () => {
    const { chart, paths } = await show();
    const cols = columns(chart);
    const last = cols.at(-1);
    if (last === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(last);
    const tip = screen.getByRole("tooltip");
    const end = paths.future.at(-1);
    expect(tip).toHaveTextContent("3 years out");
    expect(tip).toHaveTextContent(`Baseline EPS${usd(end?.baseline.value)}`);
    expect(tip).toHaveTextContent(`Required EPS${usd(end?.required.value)}`);
    expect(tip).toHaveTextContent(`Gap-${usd(Math.abs(end?.gap.value ?? 0))}`);
  });

  it("hides the guide line when the pointer leaves the chart", async () => {
    const { chart } = await show();
    const col = columns(chart)[0];
    if (col === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(col);
    await userEvent.unhover(chart);
    expect(guide(chart)).toHaveAttribute("visibility", "hidden");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("moves between columns with the arrow keys", async () => {
    const { chart, paths } = await show();
    act(() => {
      chart.focus();
    });
    const today = paths.history.at(-1);
    expect(screen.getByRole("tooltip")).toHaveTextContent(`${today?.label ?? ""} (today)`);
    fireEvent.keyDown(chart, { key: "ArrowLeft" });
    expect(screen.getByRole("tooltip")).toHaveTextContent(paths.history.at(-2)?.label ?? "");
    fireEvent.keyDown(chart, { key: "End" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("3 years out");
  });

  it("names a ten-year projection in full", async () => {
    const { chart } = await show(leversWith({ type: "setHoldingYears", value: 10 }));
    const last = columns(chart).at(-1);
    if (last === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(last);
    expect(screen.getByRole("tooltip")).toHaveTextContent("10 years out");
  });

  it("updates an open tooltip when a lever changes", async () => {
    const probe = evaluationProbe();
    await renderReady(
      <>
        <probe.Probe />
        <RaiseEps />
        <WhenReady>
          <EarningsTrajectoryPanel />
        </WhenReady>
      </>,
    );
    const chart = screen.getByRole("img", { name: /TTM EPS/ });
    const last = columns(chart).at(-1);
    if (last === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(last);
    fireEvent.click(screen.getByRole("button", { name: "Raise EPS" }));
    await waitFor(() => {
      expect(probe.current().earningsPaths.value?.future[0]?.baseline.value).toBe(12);
    });
    const end = probe.current().earningsPaths.value?.future.at(-1);
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      `Baseline EPS${usd(end?.baseline.value)}`,
    );
  });

  it("closes the tooltip of a column that a shorter holding period removes", async () => {
    const { chart } = await show();
    const last = columns(chart).at(-1);
    if (last === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(last);
    fireEvent.click(screen.getByRole("button", { name: "One year" }));
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    });
    const now = screen.getByRole("img", { name: /TTM EPS/ });
    expect(guide(now)).toHaveAttribute("visibility", "hidden");
  });

  it("closes the tooltip when the chart goes away", async () => {
    const { chart } = await show();
    const col = columns(chart)[2];
    if (col === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(col);
    fireEvent.click(screen.getByRole("button", { name: "Negative EPS" }));
    await waitFor(() => {
      expect(screen.getByText("Needs positive EPS to project.")).toBeInTheDocument();
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("keeps a tapped tooltip when the finger lifts", async () => {
    const { chart } = await show();
    const col = columns(chart)[3];
    if (col === undefined) {
      throw new Error("no column");
    }
    fireEvent.pointerDown(col, { pointerType: "touch" });
    fireEvent.pointerLeave(chart, { pointerType: "touch" });
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
    expect(guide(chart)).toHaveAttribute("visibility", "visible");
  });

  it("announces the column values that the arrow keys reach", async () => {
    const { chart, paths } = await show();
    act(() => {
      chart.focus();
    });
    fireEvent.keyDown(chart, { key: "ArrowLeft" });
    const q = paths.history.at(-2);
    expect(screen.getByRole("status")).toHaveTextContent(
      `${q?.label ?? ""}Reported TTM EPS${usd(q?.ttmEps.value)}`,
    );
  });

  it("starts the projection at today when the stock has no reported quarters", async () => {
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:ticker/history"), () =>
        HttpResponse.json({
          ...historyFixture,
          history: { ...historyFixture.history, quarters: [] },
          quarters: [],
        }),
      ),
    );
    const { chart, paths } = await show();
    expect(paths.history).toHaveLength(0);
    const cols = columns(chart);
    expect(cols).toHaveLength(paths.future.length);
    const first = cols[0];
    if (first === undefined) {
      throw new Error("no column");
    }
    expect(Number(first.dataset.x)).toBeGreaterThan(0);
    await userEvent.hover(first);
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      `Baseline EPS${usd(paths.future[0]?.baseline.value)}`,
    );
  });

  it("asks for positive EPS instead of drawing the paths", async () => {
    const probe = evaluationProbe();
    await renderReady(
      <>
        <probe.Probe />
        <WhenReady>
          <EarningsTrajectoryPanel />
        </WhenReady>
      </>,
      { levers: leversWith({ type: "setStartingPoint", field: "ttmEps", value: -1 }) },
    );
    expect(screen.getByText("Needs positive EPS to project.")).toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  it("links today's TTM EPS to its source", async () => {
    const { ev } = await show();
    const note = screen.getByText(/Projections start from/);
    const link = within(note).getByRole("link");
    expect(link).toHaveTextContent(usd(ev.earningsPaths.value?.future[0]?.baseline.value));
  });
});

describe("the walkthrough of the earnings trajectory", () => {
  async function start(levers = leversWith()) {
    const shown = await show(levers);
    await userEvent.click(
      screen.getByRole("button", {
        name: "Walk me through the earnings trajectory with my numbers",
      }),
    );
    const card = screen.getByRole("region", { name: "Walkthrough of the earnings trajectory" });
    const next = () => userEvent.click(within(card).getByRole("button", { name: "Next" }));
    return { ...shown, card, next };
  }

  it("highlights the reported line first and uses the user's numbers", async () => {
    const { chart, card, paths } = await start();
    expect(chart.querySelector('path[data-walk~="hist"]')).toHaveAttribute("data-thl");
    expect(chart.querySelector('path[data-walk~="req"]')).not.toHaveAttribute("data-thl");
    expect(card).toHaveTextContent(
      `TTM EPS went from ${usd(paths.history[0]?.ttmEps.value)} in ${paths.history[0]?.label ?? ""} to ${usd(10.8)} today.`,
    );
  });

  it("explains the shading for the stock's own gap", async () => {
    const { card, next, ev } = await start();
    for (let i = 0; i < 4; i++) {
      await next();
    }
    const gap = ev.atBaseline.value?.epsGap.value ?? 0;
    expect(card).toHaveTextContent(
      `at your weighted trend of ${pct(ev.baselines.growth.value)}, EPS ends ${usd(-gap)} per share short of what the price needs`,
    );
    expect(card).toHaveTextContent(`about ${signedPct(ev.atBaseline.value?.yearlyReturn)} a year`);
  });

  it("rings the exit P/E and the baseline levers when it questions the color", async () => {
    const { card, next, ev } = await start();
    for (let i = 0; i < 5; i++) {
      await next();
    }
    expect(card).toHaveTextContent(
      `The color flips at a ${(ev.atBaseline.value?.exitPeNeeded.value ?? 0).toFixed(1)}× exit P/E`,
    );
    expect(card.closest("section")?.querySelector('[data-walk~="shade"]')).toHaveAttribute(
      "data-thl",
    );
    expect(screen.getByTestId("lever-exitPe")).toHaveAttribute("data-tside");
    expect(screen.getByTestId("lever-baseline")).toHaveAttribute("data-tside");
  });

  it("ends with a one-line take-away", async () => {
    const { card, next, ev } = await start();
    for (let i = 0; i < 6; i++) {
      await next();
    }
    const gap = ev.atBaseline.value?.epsGap.value ?? 0;
    expect(card).toHaveTextContent(
      `GRMN in one line: at your weighted trend, EPS ends ${usd(-gap)} per share short of what the price needs.`,
    );
  });
});
