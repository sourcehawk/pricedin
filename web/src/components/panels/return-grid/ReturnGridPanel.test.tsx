import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { historyFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, withApiServer } from "@/api/testing/server";
import { leversReducer, type LeverAction } from "@/state/levers";
import { useStock } from "@/state/StockProvider";
import { renderReady, testLevers } from "@/testing/render";
import { ReturnGridPanel } from "./ReturnGridPanel";

withApiServer();

// The GRMN example of testdata/engine/evaluation-example.json: price $270,
// TTM EPS $10.80, target 15%, 3 years, exit P/E 20×. Its grid has the
// columns 11× to 32× and the rows 0% to 30% in steps of 2.5%, plus the
// exact weighted-trend row at 16.0%.

/** WhenReady renders its children once the evaluation exists, as the page does. */
function WhenReady({ children }: { children: ReactNode }) {
  return useStock().evaluation === null ? null : children;
}

async function renderPanel(...actions: LeverAction[]) {
  await renderReady(
    <>
      <div data-lever="exitPe" />
      <div data-lever="baseline" />
      <WhenReady>
        <ReturnGridPanel />
      </WhenReady>
    </>,
    { levers: actions.reduce(leversReducer, testLevers()) },
  );
  return screen.getByRole("region", { name: "Return grid" });
}

function grid(panel: HTMLElement): HTMLElement {
  return within(panel).getByRole("table");
}

/** row returns the body row whose header ends with the growth label, such as "16.0%". */
function row(panel: HTMLElement, growth: string): HTMLElement {
  const th = within(grid(panel))
    .getAllByRole("rowheader")
    .find((h) => h.textContent.endsWith(growth));
  const tr = th?.closest("tr");
  if (!tr) {
    throw new Error(`no row ${growth}`);
  }
  return tr;
}

function cell(panel: HTMLElement, growth: string, column: number): HTMLElement {
  const td = row(panel, growth).querySelectorAll("td")[column];
  if (!(td instanceof HTMLElement)) {
    throw new Error(`no cell ${growth} ${String(column)}`);
  }
  return td;
}

const youColumn = 4; // 20×

describe("the return grid", () => {
  it("has a column for each exit P/E around the exit P/E in use", async () => {
    const panel = await renderPanel();
    const heads = within(grid(panel)).getAllByRole("columnheader");
    expect(heads.map((h) => h.textContent)).toEqual([
      "Exit P/E",
      "11×",
      "14×",
      "16×",
      "18×",
      "20×",
      "22×",
      "25×today",
      "28×",
      "32×",
    ]);
    expect(heads[5]).toHaveClass("colsel");
  });

  it("puts the YOU cell in the exact baseline row at the exit P/E in use", async () => {
    const panel = await renderPanel();
    const you = cell(panel, "16.0%", youColumn);
    expect(you).toHaveClass("sel");
    expect(you).toHaveTextContent("+8%");
    expect(grid(panel).querySelectorAll("td.sel")).toHaveLength(1);
  });

  it("tags the rows of the requirement and the baselines", async () => {
    const panel = await renderPanel();
    expect(row(panel, "25.0%").querySelector("th")).toHaveTextContent(/^Req25\.0%$/);
    expect(row(panel, "16.0%").querySelector("th")).toHaveTextContent(/^Wtd16\.0%$/);
    expect(row(panel, "12.5%").querySelector("th")).toHaveTextContent(/^Today12\.5%$/);
    expect(row(panel, "17.5%").querySelector("th")).toHaveTextContent(/^Trl17\.5%$/);
  });

  it("outlines a tagged row while its tag is hovered or focused", async () => {
    const panel = await renderPanel();
    const mark = () => panel.querySelector('.rowmark[data-row-tag="required"]');
    expect(mark()).not.toHaveClass("on");
    const tag = within(row(panel, "25.0%")).getByText("Req");
    await userEvent.hover(tag);
    expect(mark()).toHaveClass("on");
    await userEvent.unhover(tag);
    expect(mark()).not.toHaveClass("on");
    act(() => {
      tag.focus();
    });
    expect(mark()).toHaveClass("on");
  });

  it("keeps a row outlined while its tag keeps the focus after the pointer leaves", async () => {
    const panel = await renderPanel();
    const mark = () => panel.querySelector('.rowmark[data-row-tag="required"]');
    const tag = within(row(panel, "25.0%")).getByText("Req");
    act(() => {
      tag.focus();
    });
    await userEvent.hover(tag);
    await userEvent.unhover(tag);
    expect(mark()).toHaveClass("on");
    act(() => {
      tag.blur();
    });
    expect(mark()).not.toHaveClass("on");
  });

  it("keeps a row outlined while its tag stays hovered after it loses the focus", async () => {
    const panel = await renderPanel();
    const mark = () => panel.querySelector('.rowmark[data-row-tag="required"]');
    const tag = within(row(panel, "25.0%")).getByText("Req");
    await userEvent.hover(tag);
    act(() => {
      tag.focus();
    });
    act(() => {
      tag.blur();
    });
    expect(mark()).toHaveClass("on");
  });

  it("outlines a tagged row while its legend entry is hovered", async () => {
    const panel = await renderPanel();
    await userEvent.hover(within(panel).getByText("weighted trend"));
    expect(panel.querySelector('.rowmark[data-row-tag="weighted"]')).toHaveClass("on");
    expect(panel.querySelector('.rowmark[data-row-tag="required"]')).not.toHaveClass("on");
  });

  it("tones each cell by its yearly return against the target, at 1.5 pp", async () => {
    const panel = await renderPanel();
    // 15% growth at 25× returns 15.0% a year: on target.
    expect(cell(panel, "15.0%", 6)).toHaveClass("warn");
    // 22.5% growth at 20× returns 13.7%, 1.3 pp short: still near.
    expect(cell(panel, "22.5%", youColumn)).toHaveClass("warn");
    // 20% growth at 20× returns 11.4%, 3.6 pp short.
    expect(cell(panel, "20.0%", youColumn)).toHaveClass("bad");
    // 27.5% growth at 20× returns 18.4%, 3.4 pp above.
    expect(cell(panel, "27.5%", youColumn)).toHaveClass("good");
  });

  it("shades a cell on the 1.5 pp edge with the engine's tone", async () => {
    // 16.5% growth at a 25× exit returns 16.5% a year: 1.5 pp above the
    // 15% target, up to rounding, which the engine counts as near.
    const panel = await renderPanel(
      { type: "setBaselineMode", mode: "custom" },
      { type: "setCustomRate", value: 0.165 },
      { type: "setExitPe", value: 25 },
    );
    const you = grid(panel).querySelector("td.sel");
    expect(you).toHaveClass("warn");
    expect((you as HTMLElement).style.background).toBe("");
    expect((you as HTMLElement).style.getPropertyValue("--shade")).not.toBe("");
  });

  it("switches the cells between yearly return, total return and future price", async () => {
    const panel = await renderPanel();
    const you = () => cell(panel, "16.0%", youColumn);
    await userEvent.click(within(panel).getByRole("button", { name: "Total return" }));
    expect(you()).toHaveTextContent("+25%");
    await userEvent.click(within(panel).getByRole("button", { name: "Future price" }));
    expect(you()).toHaveTextContent("$337");
    await userEvent.click(within(panel).getByRole("button", { name: "Annual return" }));
    expect(you()).toHaveTextContent("+8%");
  });

  it("shows the calculation of a cell on hover", async () => {
    const panel = await renderPanel();
    await userEvent.hover(cell(panel, "16.0%", youColumn));
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("16.0% EPS growth a year, sell at 20×");
    expect(tip).toHaveTextContent("EPS now → in 3Y$10.80 → $16.84");
    expect(tip).toHaveTextContent("Share price at exit$16.84 × 20 = $336.72");
    expect(tip).toHaveTextContent("Total return+24.7%");
    expect(tip).toHaveTextContent("Per year+7.6%");
    expect(tip).toHaveTextContent("vs 15.0% target-7.4 pp");
    expect(tip).toHaveTextContent("Multiple shrinks from 25.0×, which costs return.");
  });

  it("moves between cells with the arrow keys and shows each calculation", async () => {
    const panel = await renderPanel();
    act(() => {
      cell(panel, "16.0%", youColumn).focus();
    });
    await userEvent.keyboard("{ArrowRight}");
    expect(cell(panel, "16.0%", youColumn + 1)).toHaveFocus();
    expect(screen.getByRole("tooltip")).toHaveTextContent("16.0% EPS growth a year, sell at 22×");
    await userEvent.keyboard("{ArrowDown}");
    expect(cell(panel, "17.5%", youColumn + 1)).toHaveFocus();
  });

  it("tags the custom rate's row and puts the YOU cell there", async () => {
    const panel = await renderPanel({ type: "setBaselineMode", mode: "custom" });
    const r = row(panel, "12.0%");
    expect(r.querySelector("th")).toHaveTextContent(/Custom/);
    expect(r.querySelector("td.sel")).not.toBeNull();
  });

  it("explains why there is no grid without positive EPS", async () => {
    const panel = await renderPanel({ type: "setStartingPoint", field: "ttmEps", value: -1 });
    expect(within(panel).queryByRole("table")).toBeNull();
    expect(panel).toHaveTextContent("The return grid needs positive EPS.");
  });

  it("names the stock and the target in its subtitle", async () => {
    const panel = await renderPanel();
    expect(panel).toHaveTextContent(
      "3-year outcomes for GRMN from $270.00 and $10.80 EPS. Colors compare each yearly return with your 15.0% target, not with zero.",
    );
  });
});

describe("the walkthrough of the return grid", () => {
  async function start() {
    const panel = await renderPanel();
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    return { panel, card: screen.getByRole("region", { name: "Walkthrough of the return grid" }) };
  }
  const next = (card: HTMLElement) =>
    userEvent.click(within(card).getByRole("button", { name: "Next" }));
  const go = async (card: HTMLElement, steps: number) => {
    for (let i = 0; i < steps; i++) {
      await next(card);
    }
  };

  it("highlights an example row first", async () => {
    const { panel, card } = await start();
    expect(card).toHaveTextContent("step 1 of 14");
    for (const td of row(panel, "15.0%").querySelectorAll("td")) {
      expect(td).toHaveAttribute("data-thl");
    }
    expect(cell(panel, "16.0%", 0)).not.toHaveAttribute("data-thl");
  });

  it("gives every step a line with the user's numbers", async () => {
    const { card } = await start();
    const lines: string[] = [];
    for (let i = 0; i < 14; i++) {
      lines.push(card.querySelector(".t-you")?.textContent ?? "");
      if (i < 13) {
        await next(card);
      }
    }
    expect(lines).toEqual([
      "Example: the 15.0% row assumes EPS grows 15.0% a year for 3 years, from $10.80 to $16.43.",
      "Your column is 20×: when you sell in 3 years, the share price is assumed to be 20 times the EPS at that point. Today's P/E is 25.0×.",
      "Here: 15.0% growth and a 20× exit. EPS reaches $16.43, so the price is $16.43 × 20 = $328.51. From today's $270.00 that is +6.8% a year.",
      "Your case: 16.0% growth (weighted trend) at 20× earns +7.6% a year, 7.4 pp below your 15.0% target.",
      "In your 20× column, every row up to 20.0% growth falls short of your target by more than 1.5 pp. You need roughly 22.5% a year before the return gets close to your 15.0% target.",
      "Your column beats your target from 27.5% growth, 11.5 points above your 16.0% baseline.",
      "In your column the rows near your target are 22.5% and 25.0%. That is where the price lands you right at your target, close to the 23.9% the price requires.",
      "Your cell falls short of your target: +7.6% a year against your 15.0% target. You reach the target at 23.9% growth (yours is 16.0%) or at a 24.4× exit P/E (yours is 20×). Neither is a small step, so the result is not fragile.",
      "At your 16.0% baseline, selling at 11× earns -11.8% a year and selling at 32× earns +25.9% a year. The exit P/E alone moves your yearly return by 38 points.",
      "In your 20× column, each 2.5% of extra growth a year adds about 2.3 points of yearly return.",
      "Today's price requires 23.9% EPS growth a year for your 15.0% target at 20×. Your baseline is 16.0%, 7.9 pp short of it.",
      "The weighted trend is 16.0%, 7.9 points short of what the price requires.",
      "The latest quarter grew 12.2%, below the weighted trend, so growth slowed recently.",
      "Over the last 12 months TTM EPS grew 16.8% a year, faster than the weighted trend: growth was stronger earlier in the period.",
    ]);
  });

  it("boxes the cell where the example row meets the user's column", async () => {
    const { panel, card } = await start();
    await go(card, 2);
    expect(cell(panel, "15.0%", youColumn)).toHaveClass("tmeet");
    expect(grid(panel).querySelectorAll(".tmeet")).toHaveLength(1);
    await next(card);
    expect(cell(panel, "16.0%", youColumn)).toHaveClass("tmeet");
    expect(cell(panel, "15.0%", youColumn)).not.toHaveClass("tmeet");
  });

  it("highlights the cells of one tone in each tone step", async () => {
    const { panel, card } = await start();
    await go(card, 4);
    expect(cell(panel, "0.0%", 0)).toHaveAttribute("data-thl");
    expect(cell(panel, "30.0%", 8)).not.toHaveAttribute("data-thl");
    await next(card);
    expect(cell(panel, "30.0%", 8)).toHaveAttribute("data-thl");
    await next(card);
    expect(cell(panel, "15.0%", 6)).toHaveAttribute("data-thl");
  });

  it("rings the exit P/E and baseline levers when it questions the user's cell", async () => {
    const { card } = await start();
    await go(card, 7);
    expect(document.querySelector('[data-lever="exitPe"]')).toHaveAttribute("data-tside");
    expect(document.querySelector('[data-lever="baseline"]')).toHaveAttribute("data-tside");
  });

  it("highlights each tag's row and ends with a take-away", async () => {
    const { panel, card } = await start();
    await go(card, 10);
    expect(cell(panel, "25.0%", 0)).toHaveAttribute("data-thl");
    await go(card, 3);
    expect(cell(panel, "17.5%", 0)).toHaveAttribute("data-thl");
    expect(card.querySelector(".t-takeaway")).toHaveTextContent(
      "GRMN in one line: the price requires 23.9% EPS growth a year for 15.0% at a 20× exit. The weighted trend is 16.0% and the latest quarter 12.2%. Your weighted trend of 16.0% falls short, so the price assumes growth speeds up.",
    );
  });

  it("names no color in any step", async () => {
    const { card } = await start();
    for (let i = 0; i < 14; i++) {
      expect(card.textContent).not.toMatch(/\b(green|amber|red|blue|purple|teal|orange)\b/i);
      if (i < 13) {
        await next(card);
      }
    }
  });
  it("says no growth is needed when the dividends alone reach the target", async () => {
    const panel = await renderPanel(
      { type: "setStartingPoint", field: "dividend", value: 130 },
      { type: "setTargetReturn", value: 0.1 },
    );
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    const card = screen.getByRole("region", { name: "Walkthrough of the return grid" });
    for (let i = 0; i < 14; i++) {
      expect(card).not.toHaveTextContent("n/a");
      if (i < 13) {
        await next(card);
      }
    }
    expect(card.querySelector(".t-takeaway")).toHaveTextContent(
      "GRMN in one line: your dividends alone reach 10.0% a year, so the price requires no EPS growth.",
    );
  });

  it("draws its conclusion from the baseline in use, and names it", async () => {
    const panel = await renderPanel(
      { type: "setBaselineMode", mode: "custom" },
      { type: "setCustomRate", value: 0.3 },
    );
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    const card = screen.getByRole("region", { name: "Walkthrough of the return grid" });
    await go(card, 13);
    expect(card.querySelector(".t-takeaway")).toHaveTextContent(
      "The weighted trend is 16.0% and the latest quarter 12.2%. Your custom growth of 30.0% clears the bar.",
    );
  });
  it("shows yearly returns during the walkthrough and gives the chosen view back after it", async () => {
    const panel = await renderPanel();
    await userEvent.click(within(panel).getByRole("button", { name: "Future price" }));
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    expect(cell(panel, "16.0%", youColumn)).toHaveTextContent("+8%");
    await userEvent.click(screen.getByRole("button", { name: "End the walkthrough" }));
    expect(cell(panel, "16.0%", youColumn)).toHaveTextContent("$337");
    expect(within(panel).getByRole("button", { name: "Future price" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("locks the view switch while the walkthrough runs", async () => {
    const panel = await renderPanel();
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    expect(within(panel).getByRole("button", { name: "Future price" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "End the walkthrough" }));
    expect(within(panel).getByRole("button", { name: "Future price" })).toBeEnabled();
  });

  it("counts the dividends in the return where the rows meet", async () => {
    const panel = await renderPanel({ type: "setStartingPoint", field: "dividend", value: 2 });
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    const card = screen.getByRole("region", { name: "Walkthrough of the return grid" });
    await go(card, 2);
    expect(card.querySelector(".t-you")).toHaveTextContent(
      /= \$328\.51, plus \$6\.00 in dividends\. From today's \$270\.00 that is \+\d+\.\d% a year\.$/,
    );
  });

  it("marks the exit P/E column and explains it when no baseline is available", async () => {
    // Four quarters give no year-over-year growth, so the weighted trend is missing.
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/GRMN/history"), () =>
        HttpResponse.json({
          ...historyFixture,
          history: {
            ...historyFixture.history,
            quarters: historyFixture.history.quarters.slice(-4),
          },
          quarters: historyFixture.quarters.slice(-4),
        }),
      ),
    );
    const panel = await renderPanel();
    expect(grid(panel).querySelector("td.sel")).toBeNull();
    expect(grid(panel).querySelector("th.colsel")).not.toBeNull();
    await userEvent.click(
      within(panel).getByRole("button", { name: "Walk me through this grid with my numbers" }),
    );
    const card = screen.getByRole("region", { name: "Walkthrough of the return grid" });
    expect(card.querySelector(".t-you")).toHaveTextContent(/^Example: the /);
    await next(card);
    expect(card.querySelector(".t-you")).toHaveTextContent(/^Your column is \d+(\.\d)?×/);
  });
});
