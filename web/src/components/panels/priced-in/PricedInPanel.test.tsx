import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { historyFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, withApiServer } from "@/api/testing/server";
import type { ReactNode } from "react";
import { leversReducer, type LeverAction } from "@/state/levers";
import { useStock } from "@/state/StockProvider";
import { renderReady, testLevers } from "@/testing/render";
import { PricedInPanel } from "./PricedInPanel";

withApiServer();

// The GRMN example of testdata/engine/evaluation-example.json: price $270,
// TTM EPS $10.80, target 15%, 3 years, estimated exit P/E 20×, weighted
// trend 16.0%, required growth 23.9%.

/** WhenReady renders its children once the evaluation exists, as the page does. */
function WhenReady({ children }: { children: ReactNode }) {
  return useStock().evaluation === null ? null : children;
}

function levers(...actions: LeverAction[]) {
  return actions.reduce(leversReducer, testLevers());
}

async function renderPanel(...actions: LeverAction[]) {
  await renderReady(
    <>
      <div data-lever="targetReturn" />
      <div data-lever="exitPe" />
      <WhenReady>
        <PricedInPanel />
      </WhenReady>
    </>,
    { levers: levers(...actions) },
  );
  return screen.getByRole("region", { name: "Priced in" });
}

function valueOf(panel: HTMLElement, term: RegExp): HTMLElement {
  const dt = within(panel)
    .getAllByRole("term")
    .find((t) => term.test(t.textContent));
  const dd = dt?.nextElementSibling;
  if (!(dd instanceof HTMLElement)) {
    throw new Error(`no value for ${String(term)}`);
  }
  return dd;
}

describe("the priced-in panel", () => {
  it("shows the required growth with the assumptions it rests on", async () => {
    const panel = await renderPanel();
    expect(within(panel).getByTestId("required-growth")).toHaveTextContent("23.9%/ yr");
    expect(panel).toHaveTextContent(
      "For 15.0% a year over 3 years, exiting at 20.0× earnings (estimated).",
    );
  });

  it("shows the exit price and the EPS needed", async () => {
    const panel = await renderPanel();
    expect(panel).toHaveTextContent("Exit share price needed$410.64+52% from today, in 3 years");
    expect(panel).toHaveTextContent("EPS needed$20.53from $10.80 today, × 20.0 exit P/E");
  });

  it("labels a baseline far below the requirement as demanding", async () => {
    const panel = await renderPanel();
    const pill = within(panel).getByText("Demanding: needs growth to accelerate");
    expect(pill).toHaveClass("pill", "bad");
  });

  it("labels a baseline well above the requirement as undemanding", async () => {
    const panel = await renderPanel({ type: "setTargetReturn", value: 0.05 });
    const pill = within(panel).getByText("Undemanding: the weighted trend clears the bar");
    expect(pill).toHaveClass("pill", "good");
  });

  it("labels a baseline within 1.5 pp of the requirement as tight", async () => {
    // At 8% over 3 years the GRMN example requires about 16.3%, within
    // 1.5 pp of the 16.0% weighted trend.
    const panel = await renderPanel({ type: "setTargetReturn", value: 0.08 });
    const pill = within(panel).getByText("Tight: needs the weighted trend to hold");
    expect(pill).toHaveClass("pill", "warn");
  });

  it("shows the three baselines, the gap and the outcome at the baseline", async () => {
    const panel = await renderPanel();
    expect(valueOf(panel, /^Weighted trend/)).toHaveTextContent("16.0%");
    expect(valueOf(panel, /^Today/)).toHaveTextContent("12.2%");
    expect(valueOf(panel, /^Trailing growth \(12 mo\)/)).toHaveTextContent("16.8%");
    const gap = valueOf(panel, /^Gap vs weighted trend/);
    expect(gap).toHaveTextContent("-7.9 pp");
    expect(gap.querySelector(".down")).not.toBeNull();
    expect(valueOf(panel, /^Implied price at baseline/)).toHaveTextContent("$336.72");
    const ret = valueOf(panel, /^Annual return at baseline/);
    expect(ret).toHaveTextContent("+7.6%");
    expect(ret.querySelector(".down")).not.toBeNull();
    expect(valueOf(panel, /^Exit P\/E needed at baseline/)).toHaveTextContent("24.4×");
  });

  it("shows the last six quarters' YoY growth, each linked to its filing", async () => {
    const panel = await renderPanel();
    const chips = within(panel).getByRole("list", { name: "Quarterly EPS growth, year over year" });
    const items = within(chips).getAllByRole("listitem");
    expect(items.map((i) => i.textContent)).toEqual([
      "Q2 FY25 +15%",
      "Q3 FY25 +11%",
      "Q4 FY25 +24%",
      "Q1 FY26 +17%",
      "Q2 FY26 +13%",
      "Q3 FY26 +12%",
    ]);
    expect(within(items[5] as HTMLElement).getByRole("link")).toHaveAttribute(
      "href",
      "https://www.sec.gov/Archives/edgar/data/0/2026-09-30.htm",
    );
  });

  it("places the requirement and the baselines on the growth scale", async () => {
    const panel = await renderPanel();
    const scale = within(panel).getByRole("img", { name: /^Growth scale/ });
    for (const label of ["Required 23.9%", "Weighted 16.0%", "Today 12.2%", "Trailing 12m 16.8%"]) {
      expect(within(scale).getByText(label)).toBeInTheDocument();
    }
  });

  it("leaves the custom rate off the scale for the other baselines", async () => {
    const panel = await renderPanel();
    expect(within(panel).queryByText(/^Custom /)).toBeNull();
  });

  it("shows the custom rate on the scale for the custom baseline", async () => {
    const panel = await renderPanel({ type: "setBaselineMode", mode: "custom" });
    expect(within(panel).getByText("Custom 12.0%")).toBeInTheDocument();
  });

  it("names the custom rate in the growth scale's accessible name", async () => {
    const panel = await renderPanel({ type: "setBaselineMode", mode: "custom" });
    expect(within(panel).getByRole("img", { name: /^Growth scale/ })).toHaveAccessibleName(
      /and your custom growth 12\.0%$/,
    );
  });

  it("explains a missing requirement instead of showing a number", async () => {
    const panel = await renderPanel({ type: "setStartingPoint", field: "ttmEps", value: -1 });
    expect(within(panel).getByTestId("required-growth")).toHaveTextContent("n/a");
    expect(panel).toHaveTextContent(
      "A P/E model needs positive EPS. Loss-making companies need a different method.",
    );
    expect(panel.querySelector(".pill")).toBeNull();
    expect(within(panel).queryByText("Exit share price needed")).toBeNull();
    expect(valueOf(panel, /^Gap vs/)).toHaveTextContent("n/a");
  });

  it("says no growth is needed when the dividends alone reach the target", async () => {
    const panel = await renderPanel(
      { type: "setStartingPoint", field: "dividend", value: 130 },
      { type: "setTargetReturn", value: 0.1 },
    );
    expect(within(panel).getByTestId("required-growth")).toHaveTextContent("none/ yr");
    expect(
      within(panel).getByText("Undemanding: your dividends alone reach the target"),
    ).toHaveClass("good");
  });

  it("links a YoY chip whose quarter EPS was derived to the newest filing behind it", async () => {
    const quarters = historyFixture.quarters.map((q, i, all) => {
      if (i !== all.length - 1 || q.eps?.kind !== "sec") {
        return q;
      }
      const fy = { ...q.eps, form: "10-K", filed: "2026-11-02", url: "https://www.sec.gov/fy.htm" };
      return {
        ...q,
        eps: {
          kind: "derived" as const,
          method: "fy_difference" as const,
          estimate: false,
          inputs: [fy, q.eps],
        },
      };
    });
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/GRMN/history"), () =>
        HttpResponse.json({ ...historyFixture, quarters }),
      ),
    );
    const panel = await renderPanel();
    const chips = within(panel).getByRole("list", { name: "Quarterly EPS growth, year over year" });
    const last = within(chips).getAllByRole("listitem").at(-1) as HTMLElement;
    expect(within(last).getByRole("link")).toHaveAttribute("href", "https://www.sec.gov/fy.htm");
  });

  it("explains a missing requirement for negative EPS also with a custom exit P/E", async () => {
    const panel = await renderPanel(
      { type: "setExitPe", value: 20 },
      { type: "setStartingPoint", field: "ttmEps", value: -1 },
    );
    expect(within(panel).getByTestId("required-growth")).toHaveTextContent("n/a");
    expect(panel).toHaveTextContent(
      "A P/E model needs positive EPS. Loss-making companies need a different method.",
    );
    expect(panel).not.toHaveTextContent("exiting at");
  });
});

describe("the walkthrough of the priced-in summary", () => {
  async function start(...actions: LeverAction[]) {
    const panel = await renderPanel(...actions);
    await userEvent.click(
      within(panel).getByRole("button", {
        name: "Walk me through the priced-in summary with my numbers",
      }),
    );
    return {
      panel,
      card: screen.getByRole("region", { name: "Walkthrough of the priced-in summary" }),
    };
  }
  const next = (card: HTMLElement) =>
    userEvent.click(within(card).getByRole("button", { name: "Next" }));

  it("highlights the big number first, with the user's requirement", async () => {
    const { panel, card } = await start();
    expect(card).toHaveTextContent("step 1 of 7");
    expect(within(panel).getByTestId("required-growth")).toHaveAttribute("data-thl");
    expect(card).toHaveTextContent(
      "GRMN must grow EPS 23.9% a year on average for 3 years to give you 15.0% a year.",
    );
  });

  it("gives every step a line with the user's numbers", async () => {
    const { card } = await start();
    const lines: string[] = [];
    for (let i = 0; i < 7; i++) {
      lines.push(card.querySelector(".t-you")?.textContent ?? "");
      if (i < 6) {
        await next(card);
      }
    }
    expect(lines).toEqual([
      "GRMN must grow EPS 23.9% a year on average for 3 years to give you 15.0% a year.",
      "In dollars: EPS must go from $10.80 today to $20.53 in 3 years. At 20.0× that is a share price of $410.64, which turns today's $270.00 into 15.0% a year.",
      "Weighted 16.0%, today 12.2%, trailing 16.8%. All three are below the 23.9% the price requires.",
      "Your baseline (weighted trend, 16.0%) is -7.9 pp from the requirement. It needs a clear acceleration, which your baseline does not show.",
      "GRMN shows Demanding: your baseline (weighted trend, 16.0%) is -7.9 pp from the 23.9% required. It would become Tight at about 22.4% growth.",
      "The last four quarters grew +24%, +17%, +13%, +12%. Growth is slowing, so expect the weighted trend to keep drifting down.",
      "Try it: a 18.0× exit would require 28.3%, and a 13.0% target would require 21.7%, against 23.9% now.",
    ]);
  });

  it("highlights the baselines and their marks on the scale when it compares them", async () => {
    const { panel, card } = await start();
    await next(card);
    await next(card);
    expect(valueOf(panel, /^Weighted trend/)).toHaveAttribute("data-thl");
    expect(valueOf(panel, /^Gap vs/)).not.toHaveAttribute("data-thl");
    const scale = within(panel).getByRole("img", { name: /^Growth scale/ });
    expect(within(scale).getByText("Weighted 16.0%")).toHaveAttribute("data-thl");
  });

  it("rings the target return and exit P/E levers and ends with a take-away", async () => {
    const { card } = await start();
    for (let i = 0; i < 6; i++) {
      await next(card);
    }
    expect(document.querySelector('[data-lever="targetReturn"]')).toHaveAttribute("data-tside");
    expect(document.querySelector('[data-lever="exitPe"]')).toHaveAttribute("data-tside");
    expect(card.querySelector(".t-takeaway")).toHaveTextContent(
      "GRMN in one line: the price requires 23.9% EPS growth a year; your weighted trend is 16.0%. The price assumes faster growth than your baseline.",
    );
    expect(within(card).getByRole("button", { name: "Finish" })).toBeInTheDocument();
  });

  it("shows label samples in the verdict step", async () => {
    const { card } = await start();
    for (let i = 0; i < 4; i++) {
      await next(card);
    }
    const you = card.querySelector(".t-you");
    expect(you?.querySelector(".pill.bad")).toHaveTextContent("Demanding");
    expect(you?.querySelector(".pill.warn")).toHaveTextContent("Tight");
  });

  it("names no color in any step", async () => {
    const { card } = await start();
    for (let i = 0; i < 7; i++) {
      expect(card.textContent).not.toMatch(/\b(green|amber|red|blue|purple|teal|orange)\b/i);
      if (i < 6) {
        await next(card);
      }
    }
  });
  it("keeps an undemanding label only while the baseline stays 1.5 pp above the requirement", async () => {
    const { card } = await start({ type: "setTargetReturn", value: 0.05 });
    for (let i = 0; i < 4; i++) {
      await next(card);
    }
    expect(card.querySelector(".t-you")).toHaveTextContent(
      /It stays Undemanding while the baseline stays above about \d+\.\d% growth\.$/,
    );
  });

  it("says no growth is needed in every step when the dividends alone reach the target", async () => {
    const { card } = await start(
      { type: "setStartingPoint", field: "dividend", value: 130 },
      { type: "setTargetReturn", value: 0.1 },
    );
    expect(card.querySelector(".t-you")).toHaveTextContent(
      "GRMN needs no EPS growth: your dividends alone reach 10.0% a year.",
    );
    for (let i = 0; i < 7; i++) {
      expect(card).not.toHaveTextContent("n/a");
      if (i < 6) {
        await next(card);
      }
    }
  });

  it("draws its conclusion from the baseline in use, and names it", async () => {
    const { card } = await start(
      { type: "setBaselineMode", mode: "custom" },
      { type: "setCustomRate", value: 0.3 },
    );
    for (let i = 0; i < 6; i++) {
      await next(card);
    }
    expect(card.querySelector(".t-takeaway")).toHaveTextContent(
      "GRMN in one line: the price requires 23.9% EPS growth a year; your custom growth is 30.0% and the weighted trend 16.0%. Your baseline clears the bar.",
    );
  });
  it("says that nothing can be compared when the requirement is missing", async () => {
    const { card } = await start({ type: "setStartingPoint", field: "ttmEps", value: -1 });
    await next(card);
    await next(card);
    expect(card.querySelector(".t-you")).toHaveTextContent(
      "The price's requirement cannot be computed, so there is nothing to compare the baselines with.",
    );
  });

  it("counts the dividends in the dollars it takes", async () => {
    const { card } = await start({ type: "setStartingPoint", field: "dividend", value: 2 });
    await next(card);
    expect(card.querySelector(".t-you")).toHaveTextContent(
      /that is a share price of \$\d[\d,.]*, plus \$6\.00 in dividends, which turns today's \$270\.00 into 15\.0% a year\.$/,
    );
  });

  it("reads a positive gap as clearing the requirement, also when the label is Tight", async () => {
    const { card } = await start(
      { type: "setBaselineMode", mode: "custom" },
      { type: "setCustomRate", value: 0.25 },
    );
    for (let i = 0; i < 3; i++) {
      await next(card);
    }
    expect(card.querySelector(".t-you")).toHaveTextContent(
      "Your baseline (custom growth, 25.0%) is +1.1 pp from the requirement. It clears the requirement, but by less than 1.5 pp.",
    );
  });
});
