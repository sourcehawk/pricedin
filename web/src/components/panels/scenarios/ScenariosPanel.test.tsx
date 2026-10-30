import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { signedPct, usd } from "@/components/format";
import type { ScenarioCase } from "@/engine";
import { evaluationProbe, leversWith, renderReady, WhenReady } from "@/testing/render";
import { ScenariosPanel } from "./ScenariosPanel";

withApiServer();

async function show(levers = leversWith()) {
  const probe = evaluationProbe();
  await renderReady(
    <>
      <probe.Probe />
      <WhenReady>
        <ScenariosPanel />
      </WhenReady>
    </>,
    { levers },
  );
  return { probe, ev: probe.current() };
}

function card(name: string): HTMLElement {
  return screen.getByRole("group", { name });
}

/** The cases where only Bear meets a 15% target: the outcomes in reverse order. */
const reversed: ScenarioCase[] = [
  { name: "Bear", growth: 0.3, exitPe: 30 },
  { name: "Base", growth: 0.05, exitPe: 20 },
  { name: "Bull", growth: 0, exitPe: 15 },
];

describe("ScenariosPanel", () => {
  it("shows the bear, base and bull cases with their inputs", async () => {
    const { ev } = await show();
    for (const c of ev.scenarios.cases) {
      const g = within(card(c.name));
      expect(g.getByLabelText("EPS growth %/yr")).toHaveValue(Number((c.growth * 100).toFixed(2)));
      expect(g.getByLabelText("Exit P/E")).toHaveValue(c.exitPe);
    }
  });

  it("shows each case's yearly return, exit EPS, share price and total return", async () => {
    const { ev } = await show();
    for (const c of ev.scenarios.cases) {
      const r = c.result.value;
      const g = within(card(c.name));
      expect(g.getByText(signedPct(r?.yearlyReturn))).toBeInTheDocument();
      expect(g.getByText(usd(r?.eps))).toBeInTheDocument();
      expect(g.getByText(usd(r?.exitPrice))).toBeInTheDocument();
      expect(g.getByText(signedPct(r?.totalReturn))).toBeInTheDocument();
    }
  });

  it("marks the cases against the target return", async () => {
    const { ev } = await show();
    const [bearCase, , bullCase] = ev.scenarios.cases;
    const bear = within(card("Bear"));
    const bull = within(card("Bull"));
    expect(bear.getByText("Misses target")).toHaveClass("pill", "bad");
    expect(bear.getByText(signedPct(bearCase?.result.value?.yearlyReturn))).toHaveClass(
      "ret",
      "down",
    );
    expect(bull.getByText("Beats target")).toHaveClass("pill", "good");
    expect(bull.getByText(signedPct(bullCase?.result.value?.yearlyReturn))).toHaveClass(
      "ret",
      "up",
    );
  });

  it("recomputes a case when the user types a growth rate", async () => {
    const { probe } = await show();
    const input = within(card("Bear")).getByLabelText("EPS growth %/yr");
    await userEvent.clear(input);
    await userEvent.type(input, "25");
    await waitFor(() => {
      expect(probe.current().scenarios.cases[0]?.growth).toBe(0.25);
    });
    const r = probe.current().scenarios.cases[0]?.result.value;
    expect(within(card("Bear")).getByText(signedPct(r?.yearlyReturn))).toBeInTheDocument();
  });

  it("recomputes a case when the user types an exit P/E", async () => {
    const { probe } = await show();
    const input = within(card("Bull")).getByLabelText("Exit P/E");
    await userEvent.clear(input);
    await userEvent.type(input, "12");
    await waitFor(() => {
      expect(probe.current().scenarios.cases[2]?.exitPe).toBe(12);
    });
    expect(within(card("Bull")).getByText("Misses target")).toBeInTheDocument();
  });

  it("shows n/a for the returns without positive EPS", async () => {
    await show(leversWith({ type: "setStartingPoint", field: "ttmEps", value: -1 }));
    expect(within(card("Base")).getAllByText("n/a").length).toBeGreaterThan(0);
  });
});

describe("the walkthrough of the three cases", () => {
  async function start(levers = leversWith()) {
    const shown = await show(levers);
    await userEvent.click(
      screen.getByRole("button", { name: "Walk me through the three cases with my numbers" }),
    );
    const tour = screen.getByRole("region", {
      name: "Walkthrough of the bear, base and bull cases",
    });
    const next = () => userEvent.click(within(tour).getByRole("button", { name: "Next" }));
    return { ...shown, tour, next };
  }

  it("highlights the bear case first with its own numbers", async () => {
    const { tour, ev } = await start();
    expect(card("Bear")).toHaveAttribute("data-thl");
    expect(card("Bull")).not.toHaveAttribute("data-thl");
    const r = ev.scenarios.cases[0]?.result.value;
    expect(tour).toHaveTextContent(
      `Bear: 10.0% growth a year and a 15.0× exit P/E. $270.00 becomes ${usd(r?.exitPrice)} in 3 years, ${signedPct(r?.yearlyReturn)} a year, below your 15.0% target.`,
    );
  });

  it("names the cases that meet the target", async () => {
    const { tour, next } = await start();
    for (let i = 0; i < 3; i++) {
      await next();
    }
    expect(tour).toHaveTextContent("Bull beats your target.");
  });

  it("names the actual passing case when the outcomes are reordered", async () => {
    const { tour, next } = await start(leversWith({ type: "setScenarios", cases: reversed }));
    for (let i = 0; i < 3; i++) {
      await next();
    }
    expect(tour).toHaveTextContent("Bear beats your target.");
    await next();
    expect(tour).toHaveTextContent(
      "GRMN in one line: Bear beats your target, but Base and Bull do not.",
    );
  });

  it("ends with the worst case in the take-away", async () => {
    const { tour, next, ev } = await start();
    for (let i = 0; i < 4; i++) {
      await next();
    }
    expect(tour).toHaveTextContent(
      `GRMN in one line: Bull beats your target, but Bear and Base do not. Worst case: ${signedPct(ev.scenarios.worstYearlyReturn.value)} a year.`,
    );
  });

  it("says that no case has a return without positive EPS", async () => {
    const { tour, next } = await start(
      leversWith({ type: "setStartingPoint", field: "ttmEps", value: -1 }),
    );
    for (let i = 0; i < 3; i++) {
      await next();
    }
    expect(tour).toHaveTextContent("No case has a return: returns need positive EPS.");
    await next();
    expect(tour).toHaveTextContent(
      "GRMN in one line: no case has a return, because returns need positive EPS.",
    );
    expect(tour).not.toHaveTextContent("best case");
    expect(tour).not.toHaveTextContent("Worst case");
  });

  it("names a case without a return apart from the cases that miss", async () => {
    const cases: ScenarioCase[] = [
      { name: "Bear", growth: 0.1, exitPe: 15 },
      { name: "Base", growth: 0.3, exitPe: 30 },
      { name: "Bull", growth: 20, exitPe: 24 },
    ];
    const { tour, next } = await start(leversWith({ type: "setScenarios", cases }));
    for (let i = 0; i < 4; i++) {
      await next();
    }
    expect(tour).toHaveTextContent(
      "GRMN in one line: Base beats your target, but Bear does not. Bull has no return.",
    );
  });

  it("says when no case meets the target", async () => {
    const { tour, next } = await start(leversWith({ type: "setTargetReturn", value: 0.5 }));
    for (let i = 0; i < 4; i++) {
      await next();
    }
    expect(tour).toHaveTextContent("GRMN in one line: no case beats your target");
  });
});
