import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import {
  filedOn,
  filingUrl,
  financialsHistory,
  serveHistory,
  renderPanel,
  type HistoryQuarter,
} from "./testing";

withApiServer();

beforeEach(() => {
  serveHistory(financialsHistory());
});

const latestFiling = filingUrl("0001121788-26-090001");

/** lastQuarter changes the newest quarter: Q3 FY26. */
function lastQuarter(change: Partial<HistoryQuarter>) {
  return (qs: HistoryQuarter[]) =>
    qs.map((q, i) => (i === qs.length - 1 ? { ...q, ...change } : q));
}

function tile(name: string): HTMLElement {
  const t = screen.getByText(name, { selector: ".tile .eyebrow span" }).closest(".tile");
  if (!(t instanceof HTMLElement)) {
    throw new Error(`no tile ${name}`);
  }
  return t;
}

async function pick(name: string) {
  await userEvent.click(
    within(screen.getByRole("group", { name: "Financial metric" })).getByRole("button", { name }),
  );
}

describe("FinancialsPanel metric chart", () => {
  it("shows revenue by default, one value per quarter", async () => {
    await renderPanel();
    expect(screen.getByRole("button", { name: "Revenue" })).toHaveAttribute("aria-pressed", "true");
    const chart = screen.getByRole("group", { name: /Quarterly revenue of GRMN/ });
    expect(within(chart).getByText("$2.0B")).toBeInTheDocument();
    expect(
      within(chart).getByText("$1.8B", { selector: "a[aria-label^='Q4 FY24'] text" }),
    ).toBeInTheDocument();
    expect(within(chart).getByText("Q3'26")).toBeInTheDocument();
  });

  it("links each quarterly value to the filing it comes from", async () => {
    await renderPanel();
    const link = screen.getByRole("link", { name: /^Q3 FY26 Revenue: \$2\.0B/ });
    expect(link).toHaveAttribute("href", latestFiling);
    act(() => {
      link.focus();
    });
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent(`Form 10-Q, filed ${filedOn("2026-09-30")}`);
    expect(tip).toHaveTextContent("Accession 0001121788-26-090001");
  });

  it("shows n/a without a link for a quarter that has no value", async () => {
    serveHistory(financialsHistory(lastQuarter({ revenue: null })));
    await renderPanel();
    const chart = screen.getByRole("group", { name: /Quarterly revenue/ });
    expect(within(chart).getByText("n/a")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /^Q3 FY26 Revenue/ })).not.toBeInTheDocument();
  });

  it("colors each change against a year earlier by whether it helps", async () => {
    await renderPanel();
    expect(screen.getByText("+12% YoY", { selector: "svg text" })).toHaveClass("up");
    await pick("FCF");
    expect(screen.getByText("-31% YoY", { selector: "svg text" })).toHaveClass("down");
  });

  it("has no change for the first four quarters, which have no year earlier", async () => {
    await renderPanel();
    const chart = screen.getByRole("group", { name: /Quarterly revenue/ });
    expect(within(chart).getAllByText(/YoY$/)).toHaveLength(4);
  });

  it("shows rising operating costs in amber when they outgrow revenue, never in green", async () => {
    serveHistory(financialsHistory(lastQuarter({ operatingCosts: 1_800_000_000 })));
    await renderPanel();
    await pick("Op. costs");
    const chart = screen.getByRole("group", { name: /Quarterly operating costs/ });
    expect(within(chart).getByText("+36% YoY")).toHaveClass("warnc");
    for (const label of within(chart).getAllByText(/YoY$/)) {
      expect(label).not.toHaveClass("up");
    }
  });

  it("shows rising operating costs that keep pace with revenue in a neutral tone", async () => {
    await renderPanel();
    await pick("Op. costs");
    expect(screen.getByText("+4% YoY", { selector: "svg text" })).toHaveClass("muted");
  });

  it("zooms the share count in and colors a rise from the previous quarter as bad", async () => {
    serveHistory(financialsHistory(lastQuarter({ dilutedShares: 200_000_000 })));
    await renderPanel();
    await pick("Shares");
    const chart = screen.getByRole("group", { name: /Quarterly diluted shares/ });
    expect(within(chart).getByText(/scale zoomed in, not from zero/)).toBeInTheDocument();
    expect(within(chart).getByText("+4.2% QoQ")).toHaveClass("down");
    expect(within(chart).getByText("-2.1% QoQ")).toHaveClass("up");
  });

  it("shows a share count change that rounds to zero in a neutral tone", async () => {
    await renderPanel();
    await pick("Shares");
    const chart = screen.getByRole("group", { name: /Quarterly diluted shares/ });
    for (const label of within(chart).getAllByText("0.0% QoQ")) {
      expect(label).toHaveClass("muted");
    }
  });

  it("snaps the tooltip to the hovered quarter and draws a guide line there", async () => {
    const { container } = await renderPanel();
    const columns = container.querySelectorAll("rect.cmpcol");
    expect(columns).toHaveLength(8);
    fireEvent.pointerEnter(columns[7] as Element, { pointerType: "mouse", clientX: 5, clientY: 5 });
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Q3 FY26 Revenue");
    expect(tip).toHaveTextContent("vs Q3 FY25+12.0%");
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
    fireEvent.pointerLeave(columns[7] as Element, { pointerType: "mouse" });
    expect(container.querySelector("line.col-guide")).not.toBeInTheDocument();
  });

  it("keeps the guide line on a quarter while its value has the pointer or the focus", async () => {
    const { container } = await renderPanel();
    const link = screen.getByRole("link", { name: /^Q3 FY26 Revenue/ });
    fireEvent.pointerEnter(link, { pointerType: "mouse", clientX: 5, clientY: 5 });
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
    fireEvent.pointerLeave(link, { pointerType: "mouse" });
    expect(container.querySelector("line.col-guide")).not.toBeInTheDocument();
    act(() => {
      link.focus();
    });
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
  });

  it("shows the operating margin of a quarter in the operating income tooltip", async () => {
    const { container } = await renderPanel();
    await pick("Op. income");
    const col = container.querySelectorAll("rect.cmpcol")[7] as Element;
    fireEvent.pointerEnter(col, { pointerType: "mouse", clientX: 5, clientY: 5 });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Margin30.8%");
  });
});

describe("FinancialsPanel tiles", () => {
  it("compares the latest quarter with the same quarter a year earlier", async () => {
    await renderPanel();
    const rev = tile("Revenue Q3 FY26");
    expect(within(rev).getByText("$1.99B")).toBeInTheDocument();
    expect(within(rev).getByText("+12% YoY")).toHaveClass("up");
    expect(within(tile("Operating income")).getByText("+34% YoY")).toHaveClass("up");
    expect(within(tile("Diluted EPS")).getByText("$2.75")).toBeInTheDocument();
    expect(within(tile("Free cash flow")).getByText("-31% YoY")).toHaveClass("down");
  });

  it("shows the operating margin and its change in percentage points", async () => {
    await renderPanel();
    const margin = tile("Operating margin");
    expect(within(margin).getByText("30.8%")).toBeInTheDocument();
    expect(within(margin).getByText("+5.0 pp")).toHaveClass("up");
  });

  it("names buybacks when the share count fell", async () => {
    await renderPanel();
    expect(within(tile("Diluted shares")).getByText("-2.0% YoY (buybacks)")).toHaveClass("up");
  });

  it("names dilution in red when the share count rose", async () => {
    serveHistory(financialsHistory(lastQuarter({ dilutedShares: 200_000_000 })));
    await renderPanel();
    expect(within(tile("Diluted shares")).getByText("+2.1% YoY (dilution)")).toHaveClass("down");
  });

  it("compares operating costs with revenue and never shows their rise in green", async () => {
    await renderPanel();
    expect(within(tile("Operating costs")).getByText("+4% YoY · revenue +12%")).toHaveClass(
      "muted",
    );
  });

  it("warns when operating costs rose faster than revenue", async () => {
    serveHistory(financialsHistory(lastQuarter({ operatingCosts: 1_800_000_000 })));
    await renderPanel();
    expect(
      within(tile("Operating costs")).getByText(
        "+36% YoY · revenue +12% (costs rose faster than sales)",
      ),
    ).toHaveClass("warnc");
  });

  it("says that costs rose while sales fell", async () => {
    serveHistory(
      financialsHistory(lastQuarter({ revenue: 1_598_400_000, operatingCosts: 1_384_950_000 })),
    );
    await renderPanel();
    expect(
      within(tile("Operating costs")).getByText(
        "+5% YoY · revenue -10% (costs rose while sales fell)",
      ),
    ).toHaveClass("warnc");
  });

  it("says that costs fell less than sales", async () => {
    serveHistory(
      financialsHistory(lastQuarter({ revenue: 1_598_400_000, operatingCosts: 1_253_050_000 })),
    );
    await renderPanel();
    expect(
      within(tile("Operating costs")).getByText(
        "-5% YoY · revenue -10% (costs fell less than sales)",
      ),
    ).toHaveClass("warnc");
  });

  it("links each latest value to its filing", async () => {
    await renderPanel();
    expect(within(tile("Revenue Q3 FY26")).getByRole("link", { name: "$1.99B" })).toHaveAttribute(
      "href",
      latestFiling,
    );
  });

  it("says why a change is missing", async () => {
    serveHistory(
      financialsHistory((qs) =>
        qs.map((q) => (q.periodEnd === "2025-09-30" ? { ...q, freeCashFlow: -10_000_000 } : q)),
      ),
    );
    await renderPanel();
    expect(
      within(tile("Free cash flow")).getByText("n/a: the year-earlier value was not positive"),
    ).toHaveClass("muted");
  });
});

describe("FinancialsPanel latest filing", () => {
  it("names the newest filing and links to it and to the company's filings on EDGAR", async () => {
    await renderPanel();
    const box = screen.getByText("Latest filing").closest(".filing") as HTMLElement;
    expect(box).toHaveTextContent(`GRMN Q3 FY26 · Form 10-Q, filed ${filedOn("2026-09-30")}`);
    expect(within(box).getByRole("link", { name: "Form 10-Q" })).toHaveAttribute(
      "href",
      latestFiling,
    );
    expect(within(box).getByRole("link", { name: "Open filings on EDGAR" })).toHaveAttribute(
      "href",
      expect.stringContaining("CIK=0001121788"),
    );
  });
});

describe("FinancialsPanel walkthrough", () => {
  async function start() {
    const r = await renderPanel();
    await userEvent.click(
      screen.getByRole("button", { name: /Walk me through the financials with my numbers/ }),
    );
    return r;
  }

  async function next(times: number) {
    for (let i = 0; i < times; i++) {
      await userEvent.click(screen.getByRole("button", { name: "Next" }));
    }
  }

  it("starts with revenue, highlighted, and the user's revenue growth", async () => {
    await start();
    const card = screen.getByRole("region", { name: "Walkthrough of the financials" });
    expect(within(card).getByRole("heading", { name: "Start with revenue" })).toBeInTheDocument();
    expect(card).toHaveTextContent("Revenue grew +12% against the same quarter a year earlier.");
    expect(tile("Revenue Q3 FY26")).toHaveAttribute("data-thl");
    expect(tile("Free cash flow")).not.toHaveAttribute("data-thl");
  });

  it("highlights operating income and the margin on the second step", async () => {
    await start();
    await next(1);
    expect(tile("Operating income")).toHaveAttribute("data-thl");
    expect(tile("Operating margin")).toHaveAttribute("data-thl");
    expect(screen.getByRole("region", { name: /Walkthrough/ })).toHaveTextContent(
      "the margin moved from 25.7% to 30.8%",
    );
  });

  it("points at the Compare switch", async () => {
    await start();
    await next(6);
    expect(screen.getByRole("heading", { name: "Use Compare" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Compare" })).toHaveAttribute("data-thl");
  });

  it("ties the growth back to the required growth and ends with a take-away", async () => {
    await start();
    await next(7);
    const card = screen.getByRole("region", { name: /Walkthrough/ });
    expect(card).toHaveTextContent("the price needs 23.9% EPS growth a year");
    expect(card).toHaveTextContent(
      "GRMN in one line: revenue +12%, operating income +34%, free cash flow -31% against a year earlier. Profit keeps up with the price only through wider margins.",
    );
    expect(screen.getByRole("button", { name: "Finish" })).toBeInTheDocument();
  });
});
