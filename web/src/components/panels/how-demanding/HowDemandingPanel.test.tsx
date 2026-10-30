import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { pct, signedPct, times, usd } from "@/components/format";
import { evaluationProbe, leversWith, WhenReady } from "../earnings-trajectory/testing";
import { exampleEvaluation } from "@/api/testing/fixtures";
import { columns } from "@/testing/charts";
import { renderReady } from "@/testing/render";
import { HowDemandingPanel } from "./HowDemandingPanel";

withApiServer();

async function show(levers = leversWith()) {
  const probe = evaluationProbe();
  await renderReady(
    <>
      <probe.Probe />
      <WhenReady>
        <HowDemandingPanel />
      </WhenReady>
    </>,
    { levers },
  );
  const ev = probe.current();
  return { ev, d: ev.howDemanding, chart: screen.getByRole("img", { name: /growth/ }) };
}

describe("HowDemandingPanel", () => {
  it("draws the required growth and the delivered growth", async () => {
    const { chart } = await show();
    expect(chart.querySelector('path[data-walk~="req"]')).not.toBeNull();
    expect(chart.querySelector('path[data-walk~="del"]')).not.toBeNull();
    expect(screen.getByText("Required EPS growth")).toBeInTheDocument();
    expect(screen.getByText("Delivered TTM EPS growth, YoY")).toBeInTheDocument();
  });

  it("labels today's required and delivered growth at the right edge", async () => {
    const { chart, d } = await show();
    const today = d.points.at(-1);
    expect(within(chart).getByText(pct(today?.required.value))).toBeInTheDocument();
    expect(within(chart).getByText(pct(today?.delivered.value))).toBeInTheDocument();
  });

  it("marks the most demanding quarter", async () => {
    const { chart, d } = await show();
    expect(d.peak.value?.index).not.toBe(d.points.length - 1);
    const peak = chart.querySelector('[data-walk~="peak"]');
    expect(peak).not.toBeNull();
    expect(within(chart).getByText("peak")).toBeInTheDocument();
  });

  it("says how the requirement changed over the range", async () => {
    const { ev, d } = await show();
    const first = d.points[0];
    const pe = ev.exitPe.inUse.value?.pe;
    expect(
      screen.getByText(
        `Required growth went from ${pct(first?.required.value)} in ${first?.label ?? ""} to ${pct(d.points.at(-1)?.required.value)} today, at a ${times(pe)} exit.`,
        { exact: false },
      ),
    ).toBeInTheDocument();
  });

  it("snaps the tooltip to a quarter with its inputs and draws the guide line there", async () => {
    const { chart, d } = await show();
    const col = columns(chart).at(-1);
    if (col === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(col);
    const tip = screen.getByRole("tooltip");
    const p = d.points.at(-1);
    expect(tip).toHaveTextContent(`${p?.label ?? ""} (today)`);
    expect(tip).toHaveTextContent(`Price${usd(p?.price.value)}`);
    expect(tip).toHaveTextContent(`TTM EPS${usd(p?.ttmEps.value)}`);
    expect(tip).toHaveTextContent(`P/E${times(p?.pe.value)}`);
    expect(tip).toHaveTextContent(`Required growth${pct(p?.required.value)}`);
    expect(tip).toHaveTextContent(`Delivered, YoY${signedPct(p?.delivered.value)}`);
    const line = chart.querySelector("line.vx");
    expect(line).toHaveAttribute("visibility", "visible");
    expect(line?.getAttribute("x1")).toBe(col.dataset.x);
  });

  it("leaves out the delivered growth where the history is too short", async () => {
    const { chart, d } = await show();
    expect(d.points[0]?.delivered.value).toBeNull();
    const col = columns(chart)[0];
    if (col === undefined) {
      throw new Error("no column");
    }
    await userEvent.hover(col);
    expect(screen.getByRole("tooltip")).not.toHaveTextContent("Delivered");
  });

  it("says that the dividends reach the target when no growth is required", async () => {
    await renderReady(
      <WhenReady>
        <HowDemandingPanel />
      </WhenReady>,
      { levers: leversWith({ type: "setStartingPoint", field: "dividend", value: 150 }) },
    );
    expect(
      screen.getByText(
        "Your dividends alone reach the target return, so no EPS growth is required.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Needs positive EPS.")).not.toBeInTheDocument();
  });

  it("labels no earlier quarter as today when today has no required growth", async () => {
    const { chart, d } = await show(
      leversWith(
        { type: "setExitPe", value: 20 },
        { type: "setStartingPoint", field: "ttmEps", value: -1 },
      ),
    );
    expect(d.points.at(-1)?.required.value).toBeNull();
    expect(d.points.at(-2)?.required.value).not.toBeNull();
    expect(chart.querySelector('[data-walk~="last"]')).toBeNull();
    expect(within(chart).queryByText(pct(d.points.at(-2)?.required.value))).toBeNull();
  });

  it("asks for positive EPS when no quarter has a required growth", async () => {
    await renderReady(
      <WhenReady>
        <HowDemandingPanel />
      </WhenReady>,
      {
        levers: leversWith({ type: "setStartingPoint", field: "ttmEps", value: -1 }),
      },
    );
    expect(screen.getByText("Needs positive EPS.")).toBeInTheDocument();
  });
});

describe("the walkthrough of how demanding the stock has been", () => {
  async function start(levers = leversWith()) {
    const shown = await show(levers);
    await userEvent.click(
      screen.getByRole("button", { name: "Walk me through how demanding the stock has been" }),
    );
    const card = screen.getByRole("region", {
      name: "Walkthrough of how demanding the stock has been",
    });
    const next = () => userEvent.click(within(card).getByRole("button", { name: "Next" }));
    return { ...shown, card, next };
  }

  it("starts with the required line and the user's numbers", async () => {
    const { chart, card, d } = await start();
    expect(chart.querySelector('path[data-walk~="req"]')).toHaveAttribute("data-thl");
    expect(chart.querySelector('path[data-walk~="del"]')).not.toHaveAttribute("data-thl");
    expect(card).toHaveTextContent(
      `Bought in ${d.points[0]?.label ?? ""}, you would have needed ${pct(d.points[0]?.required.value)} a year. Today you need ${pct(d.points.at(-1)?.required.value)}.`,
    );
  });

  it("counts the quarters where the price asked for more than the business delivered", async () => {
    const { card, next, d } = await start();
    for (let i = 0; i < 3; i++) {
      await next();
    }
    expect(card).toHaveTextContent(
      `In ${String(d.requiredAboveDelivered)} of ${String(d.compared)} quarters the price asked for more growth`,
    );
  });

  it("calls today the most demanding point when it ties with an earlier peak", async () => {
    const peak = exampleEvaluation.howDemanding.peak.value;
    if (peak === null) {
      throw new Error("the example has a peak");
    }
    // The price that requires the peak growth at a 20x exit, a hair lower
    // so that the engine keeps the earlier quarter as the peak.
    const price = ((10.8 * 20 * Math.pow(1 + peak.growth, 3)) / Math.pow(1.15, 3)) * (1 - 1e-12);
    const { card, next, d } = await start(
      leversWith(
        { type: "setExitPe", value: 20 },
        { type: "setStartingPoint", field: "price", value: price },
      ),
    );
    expect(d.peak.value?.index).toBe(peak.index);
    for (let i = 0; i < 4; i++) {
      await next();
    }
    expect(card).toHaveTextContent("is the most demanding point in the range.");
  });

  it("does not claim that each point uses only the earnings known on its date", async () => {
    const { card, next } = await start();
    for (let i = 0; i < 4; i++) {
      await next();
    }
    expect(card).not.toHaveTextContent("known on that date");
    expect(card).toHaveTextContent("it is not a record of what a buyer knew on that date");
  });

  it("compares today with the peak and ends with a take-away", async () => {
    const { chart, card, next, d } = await start();
    for (let i = 0; i < 4; i++) {
      await next();
    }
    const peak = d.peak.value;
    expect(card).toHaveTextContent(
      `Today's ${pct(d.points.at(-1)?.required.value)} is below the peak of ${pct(peak?.growth)} in ${d.points[peak?.index ?? 0]?.label ?? ""}.`,
    );
    expect(chart.querySelector('[data-walk~="peak"]')).toHaveAttribute("data-thl");
    expect(card).toHaveTextContent(
      `GRMN in one line: buying today needs ${pct(d.points.at(-1)?.required.value)} growth a year, more than the typical ${pct(d.typicalRequired.value)} over this period`,
    );
  });
});
