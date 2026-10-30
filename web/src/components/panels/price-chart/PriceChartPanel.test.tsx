import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { StockHistory } from "@/api/types";
import { historyFixture } from "@/api/testing/fixtures";
import { serveHistory, withApiServer } from "@/api/testing/server";
import { renderReady, WhenReady } from "@/testing/render";
import { PriceChartPanel } from "./PriceChartPanel";

withApiServer();

/** serveEngineHistory makes the fake API answer with the history fixture, its engine history replaced by history. */
function serveEngineHistory(history: StockHistory["history"]) {
  serveHistory({ ...historyFixture, history });
}

const panel = () => screen.getByRole("region", { name: "Price" });
const chips = () => screen.getByRole("group", { name: "Lines on the price chart" });
const chip = (name: RegExp) => within(chips()).getByRole("button", { name });
const chart = () => screen.getByRole("group", { name: /GRMN price chart/ });
const walk = (key: string) => panel().querySelectorAll(`[data-walk~="${key}"]`);

/** pointer fires a pointer event of type with the pointer type; jsdom has no PointerEvent. */
function pointer(
  el: Element | Document,
  type: string,
  pointerType: string,
  init: MouseEventInit = {},
) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, ...init });
  Object.defineProperty(e, "pointerType", { value: pointerType });
  return fireEvent(el, e);
}

/** The chart draws in a 760-unit-wide view box; a 760 px box maps client x to view box x. */
function sizeChart() {
  const svg = chart();
  svg.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 760,
    bottom: 320,
    width: 760,
    height: 320,
    toJSON: () => ({}),
  });
  return svg;
}

describe("PriceChartPanel", () => {
  it("sums up the visible range under the title", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const sub = within(panel()).getByTestId("price-sub");
    expect(sub).toHaveTextContent(
      "3Y: +164.7% · high $273.81 · low $101.55 · P/E 16.5× → 25.0× · price–earnings link: moderate (0.47)",
    );
    expect(sub).toHaveTextContent("last close $270.00 on 30 Sep 2026");
    expect(within(sub).getByText("+164.7%")).toHaveClass("up");
  });

  it("shows the latest close with its source", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const close = within(within(panel()).getByTestId("price-sub")).getByText("$270.00");
    close.focus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Close on 2026-09-30 from Massive",
    );
  });

  it("sources the latest close also when an earlier day closed at the same price", async () => {
    const daily = historyFixture.history.daily.map((d, i, all) =>
      i === all.length - 40 ? { ...d, close: all.at(-1)?.close ?? d.close } : d,
    );
    serveEngineHistory({ ...historyFixture.history, daily });
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const sub = within(panel()).getByTestId("price-sub");
    const close = within(sub).getAllByText("$270.00").at(-1) as HTMLElement;
    close.focus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Close on 2026-09-30 from Massive",
    );
  });

  it("starts with the TTM EPS, P/E and share-change overlays on", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    expect(chip(/^TTM EPS/)).toHaveAttribute("aria-pressed", "true");
    expect(chip(/^P\/E(?! bands)/)).toHaveAttribute("aria-pressed", "true");
    expect(chip(/^Share changes/)).toHaveAttribute("aria-pressed", "true");
    expect(chip(/^Price change, 12M/)).toHaveAttribute("aria-pressed", "false");
    expect(chip(/^EPS growth, 12M/)).toHaveAttribute("aria-pressed", "false");
    expect(chip(/^P\/E bands/)).toHaveAttribute("aria-pressed", "false");
    expect(walk("eps")).toHaveLength(1);
    expect(walk("r12")).toHaveLength(0);
    expect(walk("bands")).toHaveLength(0);
  });

  it("shows the last day's values in the chips", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("Price$270.00");
    expect(chip(/^TTM EPS/)).toHaveTextContent("TTM EPS$10.80");
    expect(chip(/^P\/E(?! bands)/)).toHaveTextContent("P/E25.0×");
  });

  it("toggles an overlay with its chip", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    await userEvent.click(chip(/^Price change, 12M/));
    expect(chip(/^Price change, 12M/)).toHaveAttribute("aria-pressed", "true");
    expect(chip(/^Price change, 12M/)).toHaveTextContent("+10%");
    expect(walk("r12")).toHaveLength(1);
    await userEvent.click(chip(/^TTM EPS/));
    expect(chip(/^TTM EPS/)).toHaveAttribute("aria-pressed", "false");
    expect(walk("eps")).toHaveLength(0);
  });

  it("shows the hovered day in the chips, the guide line and the tooltip", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const svg = sizeChart();
    // jsdom has no PointerEvent; React reads the coordinates of any event of this type.
    fireEvent(svg, new MouseEvent("pointermove", { bubbles: true, clientX: 50, clientY: 100 }));
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("Price$102.00");
    expect(chip(/^TTM EPS/)).toHaveTextContent("$6.20");
    expect(chip(/^P\/E(?! bands)/)).toHaveTextContent("16.5×");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "visible");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("x1", "50");
    expect(await screen.findByRole("tooltip")).toHaveTextContent("30 Sep 2023");
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "Close from Massive, adjusted for splits",
    );
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "TTM EPS adds up Q4 FY22, Q1 FY23, Q2 FY23 and Q3 FY23, known from the filings on 2023-09-30, with the values of the latest filings; the newest is Form 10-Q, filed 2023-09-30. P/E is the close over this TTM EPS.",
    );
    fireEvent.pointerLeave(svg, { pointerType: "mouse" });
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("Price$270.00");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "hidden");
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).toBeNull();
    });
  });

  it("moves through the days with the arrow keys", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const svg = chart();
    svg.focus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("$267.47");
    expect(chip(/^TTM EPS/)).toHaveTextContent("$10.50");
    await userEvent.keyboard("{ArrowRight}");
    expect(chip(/^TTM EPS/)).toHaveTextContent("$10.80");
    await userEvent.keyboard("{Home}");
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("$102.00");
  });

  it("announces the day that the arrow keys select, and not the hovered day", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const status = within(panel()).getByRole("status");
    const svg = sizeChart();
    pointer(svg, "pointermove", "mouse", { clientX: 50, clientY: 100 });
    expect(status).toHaveTextContent(/^$/);
    pointer(svg, "pointerout", "mouse");
    svg.focus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(status).toHaveTextContent("29 Sep 2026: close $267.47, TTM EPS $10.50, P/E 25.5×");
  });

  it("keeps a tapped day after the finger lifts, until a tap elsewhere", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const svg = sizeChart();
    const guide = screen.getByTestId("price-guide");
    pointer(svg, "pointerdown", "touch", { clientX: 50, clientY: 100 });
    pointer(svg, "pointerout", "touch");
    expect(guide).toHaveAttribute("visibility", "visible");
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("$102.00");
    pointer(document.body, "pointerdown", "touch");
    expect(guide).toHaveAttribute("visibility", "hidden");
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("$270.00");
  });

  it("redraws the hovered day when a line is switched on", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    pointer(sizeChart(), "pointermove", "mouse", { clientX: 50, clientY: 100 });
    expect(await screen.findByRole("tooltip")).not.toHaveTextContent("Price change, 12M");
    fireEvent.click(chip(/^Price change, 12M/));
    await waitFor(() => {
      expect(screen.getByRole("tooltip")).toHaveTextContent("Price change, 12M");
    });
  });

  it("names the period that a short history covers", async () => {
    serveEngineHistory({
      ...historyFixture.history,
      daily: historyFixture.history.daily.slice(-100),
    });
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    expect(within(chart()).getByRole("img", { name: /daily close/ })).toHaveAccessibleName(
      /^GRMN daily close since \d+ \w+ 2026, from/,
    );
    expect(within(panel()).getByTestId("price-sub")).toHaveTextContent(/^Since \d+ \w+ 2026: /);
  });

  it("drops a touch selection when the browser cancels the pointer for a scroll", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const svg = sizeChart();
    pointer(svg, "pointerdown", "touch", { clientX: 50, clientY: 100 });
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "visible");
    pointer(svg, "pointercancel", "touch");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "hidden");
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).toBeNull();
    });
  });

  it("drops a keyboard-selected day when the page scrolls", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    chart().focus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "visible");
    fireEvent.scroll(document);
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "hidden");
  });

  it("lets a user scroll of the plot drop the day when End could not scroll it", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const svg = chart();
    const box = svg.parentElement as HTMLElement;
    let scroll = 700;
    Object.defineProperty(box, "scrollWidth", { value: 1000 });
    Object.defineProperty(box, "clientWidth", { value: 300 });
    Object.defineProperty(box, "scrollLeft", {
      get: () => scroll,
      set: (v: number) => {
        scroll = Math.min(700, Math.max(0, v));
      },
    });
    svg.getBoundingClientRect = () => ({
      x: -700,
      y: 0,
      left: -700,
      top: 0,
      right: 300,
      bottom: 421,
      width: 1000,
      height: 421,
      toJSON: () => ({}),
    });
    svg.focus();
    await userEvent.keyboard("{End}");
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "visible");
    fireEvent.scroll(box);
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "hidden");
  });

  it("drops the hovered day when the range changes", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    pointer(sizeChart(), "pointermove", "mouse", { clientX: 50, clientY: 100 });
    fireEvent.click(within(panel()).getByRole("button", { name: "1Y" }));
    await waitFor(() => {
      expect(within(panel()).getByTestId("price-sub")).toHaveTextContent(/^1Y: /);
    });
    expect(screen.getByTestId("price-guide")).toHaveAttribute("visibility", "hidden");
    expect(within(chips()).getByText("Price").parentElement).toHaveTextContent("$270.00");
  });

  it("switches the range", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    const oneYear = within(panel()).getByRole("button", { name: "1Y" });
    await userEvent.click(oneYear);
    await waitFor(() => {
      expect(within(panel()).getByTestId("price-sub")).toHaveTextContent(/^1Y: /);
    });
    expect(oneYear).toHaveAttribute("aria-pressed", "true");
    expect(within(panel()).getByRole("button", { name: "3Y" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  describe("P/E bands", () => {
    async function withBands() {
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      await userEvent.click(chip(/^P\/E bands/));
      await userEvent.unhover(chip(/^P\/E bands/));
      await waitFor(() => {
        expect(screen.queryByRole("tooltip")).toBeNull();
      });
    }
    const caption = () => within(panel()).getByTestId("band-caption");
    const label = (m: string) =>
      within(chart()).getByRole("img", { name: new RegExp(`^${m} band`) });

    it("covers the P/E range of the visible period with about five bands", async () => {
      await withBands();
      const multiples = [...chart().querySelectorAll(".bline")].map((e) =>
        e.getAttribute("data-bm"),
      );
      expect(multiples).toEqual(["15", "17.5", "20", "22.5", "25", "27.5"]);
    });

    it("draws other bands for the P/E range of a shorter period", async () => {
      await withBands();
      fireEvent.click(within(panel()).getByRole("button", { name: "1Y" }));
      await waitFor(() => {
        expect(within(panel()).getByTestId("price-sub")).toHaveTextContent(/^1Y: /);
      });
      const multiples = [...chart().querySelectorAll(".bline")].map((e) =>
        Number(e.getAttribute("data-bm")),
      );
      expect(multiples).not.toEqual([15, 17.5, 20, 22.5, 25, 27.5]);
      const pes = historyFixture.history.daily
        .filter((d) => d.date >= "2025-09-30")
        .map((d) => d.close / (d.ttmEps ?? Number.NaN))
        .filter((pe) => pe > 0);
      for (const m of multiples) {
        expect(m).toBeGreaterThanOrEqual(Math.min(...pes) * 0.9);
        expect(m).toBeLessThanOrEqual(Math.max(...pes) * 1.05);
      }
    });

    it("invites a hover in the caption above the chart", async () => {
      await withBands();
      expect(caption()).toHaveTextContent(
        "Hover a dotted line or its label on the right to follow one P/E band.",
      );
      expect(caption().compareDocumentPosition(chart())).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    });

    it("lifts a hovered band line and shows its value in the caption, not in a tooltip", async () => {
      await withBands();
      const line = chart().querySelector('[data-bm="25"].bhit');
      expect(line).not.toBeNull();
      fireEvent.pointerOver(line as Element, { pointerType: "mouse" });
      expect(caption()).toHaveTextContent(
        "25× band: the share price at 25× TTM EPS. Today that is $10.80 × 25 = $270.00, right at today's price of $270.00.",
      );
      expect(chart().querySelector('[data-bm="25"].bline')).toHaveClass("bh");
      expect(chart().querySelector('[data-bm="20"].bline')).not.toHaveClass("bh");
      expect(screen.queryByRole("tooltip")).toBeNull();
      fireEvent.pointerOut(line as Element, { pointerType: "mouse" });
      await waitFor(() => {
        expect(caption()).toHaveTextContent("Hover a dotted line");
      });
      expect(chart().querySelector('[data-bm="25"].bline')).not.toHaveClass("bh");
    });

    it("keeps the band in the caption while the pointer moves onto its sources", async () => {
      await withBands();
      const line = chart().querySelector('[data-bm="25"].bhit') as Element;
      await userEvent.hover(line);
      expect(caption()).toHaveTextContent("25× band:");
      // The pointer leaves the chart for the caption above it.
      await userEvent.hover(within(caption()).getByRole("link", { name: "$10.80" }));
      await new Promise((r) => setTimeout(r, 300));
      expect(caption()).toHaveTextContent("25× band:");
      expect(chart().querySelector('[data-bm="25"].bline')).toHaveClass("bh");
      const link = within(caption()).getByRole("link", { name: "$10.80" });
      fireEvent.pointerLeave(link, { pointerType: "mouse" });
      fireEvent.pointerLeave(caption(), { pointerType: "mouse" });

      await waitFor(() => {
        expect(caption()).toHaveTextContent("Hover a dotted line");
      });
    });

    it("keeps the band while the pointer rests on the tooltip of a caption source", async () => {
      await withBands();
      await userEvent.hover(chart().querySelector('[data-bm="25"].bhit') as Element);
      const link = within(caption()).getByRole("link", { name: "$10.80" });
      await userEvent.hover(link);
      // The tooltip sits outside the caption, so the pointer leaves the caption for it.
      fireEvent.pointerLeave(link, { pointerType: "mouse" });
      fireEvent.pointerLeave(caption(), { pointerType: "mouse" });
      fireEvent.pointerEnter(screen.getByRole("tooltip"), { pointerType: "mouse" });
      await new Promise((r) => setTimeout(r, 300));
      expect(screen.getByRole("tooltip")).toHaveTextContent("Sum of the last four quarters");
      expect(caption()).toHaveTextContent("25× band:");
      fireEvent.pointerLeave(screen.getByRole("tooltip"));
      await waitFor(() => {
        expect(caption()).toHaveTextContent("Hover a dotted line");
      });
    });

    it("keeps the band while a caption source has the focus, also when the pointer leaves", async () => {
      await withBands();
      await userEvent.hover(chart().querySelector('[data-bm="25"].bhit') as Element);
      const eps = within(caption()).getByRole("link", { name: "$10.80" });
      await userEvent.hover(eps);
      act(() => {
        eps.focus();
      });
      fireEvent.pointerLeave(eps, { pointerType: "mouse" });
      fireEvent.pointerLeave(caption(), { pointerType: "mouse" });
      await new Promise((r) => setTimeout(r, 300));
      expect(caption()).toHaveTextContent("25× band:");
    });

    it("keeps the band in the caption while the focus moves onto its sources", async () => {
      await withBands();
      act(() => {
        label("25×").focus();
      });
      act(() => {
        within(caption()).getByRole("link", { name: "$10.80" }).focus();
      });
      await new Promise((r) => setTimeout(r, 300));
      expect(caption()).toHaveTextContent("25× band:");
    });

    it("sources the TTM EPS and the close in the caption", async () => {
      await withBands();
      fireEvent.pointerOver(chart().querySelector('[data-bm="25"].bhit') as Element, {
        pointerType: "mouse",
      });
      const eps = within(caption()).getByRole("link", { name: "$10.80" });
      await userEvent.hover(eps);
      const tip = screen.getByRole("tooltip");
      expect(tip).toHaveTextContent("Sum of the last four quarters");
      expect(tip.querySelectorAll(":scope > ul > li")).toHaveLength(4);
      const close = within(caption()).getAllByText("$270.00").at(-1) as HTMLElement;
      await userEvent.hover(close);
      expect(screen.getByRole("tooltip")).toHaveTextContent("Close on 2026-09-30 from Massive");
    });

    it("lifts a band whose label gets the focus", async () => {
      await withBands();
      act(() => {
        label("20×").focus();
      });
      expect(caption()).toHaveTextContent(
        "Today that is $10.80 × 20 = $216.00, below today's price",
      );
      expect(chart().querySelector('[data-bm="20"].bline')).toHaveClass("bh");
    });

    it("lets the keyboard reach a band that has no label", async () => {
      await withBands();
      const line = within(chart()).getByRole("img", { name: /^27\.5× band/ });
      expect(line).toHaveClass("bhit");
      act(() => {
        line.focus();
      });
      expect(caption()).toHaveTextContent("27.5× band:");
      expect(chart().querySelector('[data-bm="27.5"].bline')).toHaveClass("bh");
    });

    it("keeps a tapped band after the finger lifts, until a tap elsewhere", async () => {
      await withBands();
      const line = chart().querySelector('[data-bm="20"].bhit') as Element;
      pointer(line, "pointerover", "touch");
      pointer(line, "pointerout", "touch");
      expect(caption()).toHaveTextContent("20× band:");
      pointer(document.body, "pointerdown", "touch");
      await waitFor(() => {
        expect(caption()).toHaveTextContent("Hover a dotted line");
      });
    });

    it("puts the band hit areas above the layer that follows the pointer", async () => {
      await withBands();
      const capture = screen.getByTestId("price-capture");
      const hit = chart().querySelector(".bhit");
      expect(hit).not.toBeNull();
      expect(capture.compareDocumentPosition(hit as Element)).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
    });

    it("labels only the bands that reach the last day", async () => {
      const daily = historyFixture.history.daily;
      serveEngineHistory({
        ...historyFixture.history,
        daily: daily.map((d, i) => (i >= daily.length - 3 ? { ...d, ttmEps: -1 } : d)),
      });
      await withBands();
      expect(chart().querySelectorAll(".bline").length).toBeGreaterThan(0);
      expect(chart().querySelectorAll(".blab")).toHaveLength(0);
    });

    it("says why there are no bands without a positive TTM EPS", async () => {
      serveEngineHistory({
        ...historyFixture.history,
        daily: historyFixture.history.daily.map((d) => ({ ...d, ttmEps: -1 })),
      });
      await withBands();
      expect(caption()).toHaveTextContent("P/E bands need a positive TTM EPS.");
      expect(chip(/^P\/E(?! bands)/)).toHaveTextContent("n/a");
    });
  });

  describe("share-count markers", () => {
    it("marks a buyback with its change", async () => {
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByLabelText(/^Q4 FY25: buyback/);
      expect(marker).toHaveTextContent("-2.1%");
      expect(marker).toHaveClass("buyback");
    });

    it("links a marker to the filing that reports its share count", async () => {
      const url = "https://www.sec.gov/Archives/edgar/data/0/q4fy25.htm";
      serveHistory({
        ...historyFixture,
        quarters: historyFixture.quarters.map((q) =>
          q.label === "Q4 FY25" && q.eps !== null ? { ...q, dilutedShares: { ...q.eps, url } } : q,
        ),
      });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      expect(within(chart()).getByRole("link", { name: /^Q4 FY25: buyback/ })).toHaveAttribute(
        "href",
        url,
      );
    });

    it("marks a dilution with the bad tone, never the good one", async () => {
      const quarters = historyFixture.history.quarters.map((q, i, all) =>
        i === all.length - 1 ? { ...q, dilutedShares: 192_000_000 * 1.05 * 1.05 } : q,
      );
      serveEngineHistory({ ...historyFixture.history, quarters });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByLabelText(/^Q3 FY26: dilution/);
      expect(marker).toHaveClass("dilution");
      const badge = marker.querySelector("rect");
      expect(
        Number(badge?.getAttribute("x")) + Number(badge?.getAttribute("width")),
      ).toBeLessThanOrEqual(760);
    });

    it("shows the share counts on the first tap and opens the filing on the second", async () => {
      serveHistory({
        ...historyFixture,
        quarters: historyFixture.quarters.map((q) =>
          q.label === "Q4 FY25" && q.eps !== null ? { ...q, dilutedShares: q.eps } : q,
        ),
      });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByRole("link", { name: /^Q4 FY25: buyback/ });
      pointer(marker, "pointerdown", "touch");
      expect(fireEvent.click(marker)).toBe(false);
      expect(screen.getByRole("tooltip")).toHaveTextContent("Shares195.9M → 191.7M");
      pointer(marker, "pointerdown", "touch");
      expect(fireEvent.click(marker)).toBe(true);
    });

    it("says which share count is an estimate", async () => {
      serveHistory({
        ...historyFixture,
        quarters: historyFixture.quarters.map((q) =>
          q.label === "Q4 FY25" && q.eps !== null
            ? {
                ...q,
                dilutedShares: {
                  kind: "derived",
                  method: "weighted_average_q4",
                  estimate: true,
                  inputs: [q.eps],
                },
              }
            : q,
        ),
      });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByRole("link", { name: /^Q4 FY25: buyback/ });
      expect(marker).toHaveAccessibleName(
        "Q4 FY25: buyback, 195.9M to 191.7M (estimate) diluted shares, -2.1%",
      );
      fireEvent.focus(marker);
      expect(screen.getByRole("tooltip")).toHaveTextContent("Shares195.9M → 191.7M (estimate)");
    });

    it("closes its tooltip on a tap elsewhere, so the next tap is a first tap again", async () => {
      serveHistory({
        ...historyFixture,
        quarters: historyFixture.quarters.map((q) =>
          q.label === "Q4 FY25" && q.eps !== null ? { ...q, dilutedShares: q.eps } : q,
        ),
      });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByRole("link", { name: /^Q4 FY25: buyback/ });
      pointer(marker, "pointerdown", "touch");
      fireEvent.click(marker);
      expect(screen.getByRole("tooltip")).toHaveTextContent("Q4 FY25: buyback");
      pointer(chart(), "pointerdown", "touch", { clientX: 50, clientY: 100 });
      expect(screen.queryByRole("tooltip")).toBeNull();
      pointer(marker, "pointerdown", "touch");
      expect(fireEvent.click(marker)).toBe(false);
    });

    it("opens the filing with Enter after a tap", async () => {
      serveHistory({
        ...historyFixture,
        quarters: historyFixture.quarters.map((q) =>
          q.label === "Q4 FY25" && q.eps !== null ? { ...q, dilutedShares: q.eps } : q,
        ),
      });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByRole("link", { name: /^Q4 FY25: buyback/ });
      pointer(marker, "pointerdown", "touch");
      expect(fireEvent.click(marker)).toBe(false);
      marker.focus();
      // A click from the keyboard has no pointerdown before it and a detail of 0.
      expect(fireEvent.click(marker, { detail: 0 })).toBe(true);
    });

    it("keeps the share counts of a small issuer readable", async () => {
      const quarters = historyFixture.history.quarters.map((q, i, all) => ({
        ...q,
        dilutedShares: i === all.length - 1 ? 1_030_000 : 1_000_000,
      }));
      serveEngineHistory({ ...historyFixture.history, quarters });
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      expect(within(chart()).getByLabelText(/^Q3 FY26: dilution/)).toHaveAccessibleName(
        "Q3 FY26: dilution, 1.000M to 1.030M diluted shares, +3.0%",
      );
    });

    it("takes the pointer only on its badge, so the days around it stay selectable", async () => {
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      const marker = within(chart()).getByLabelText(/^Q4 FY25: buyback/);
      expect(marker.querySelector("line")).toHaveAttribute("pointer-events", "none");
      for (const r of marker.querySelectorAll("rect")) {
        expect(Number(r.getAttribute("height"))).toBeLessThan(30);
      }
    });

    it("hides with its chip", async () => {
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      await userEvent.click(chip(/^Share changes/));
      expect(within(chart()).queryByLabelText(/buyback/)).toBeNull();
    });
  });

  it("describes the chart for screen readers", async () => {
    await renderReady(
      <WhenReady>
        <PriceChartPanel />
      </WhenReady>,
    );
    expect(within(chart()).getByRole("img", { name: /daily close/ })).toHaveAccessibleName(
      "GRMN daily close over 3Y, from $102.00 to $270.00, with TTM EPS, P/E and share-change markers",
    );
  });

  describe("walkthrough", () => {
    async function start() {
      await renderReady(
        <WhenReady>
          <PriceChartPanel />
        </WhenReady>,
      );
      await userEvent.click(
        within(panel()).getByRole("button", {
          name: "Walk me through the price chart with my numbers",
        }),
      );
      return screen.getByRole("region", { name: "Walkthrough of the price chart" });
    }
    const next = async (card: HTMLElement, times = 1) => {
      for (let i = 0; i < times; i++) {
        await userEvent.click(within(card).getByRole("button", { name: /^(Next|Finish)$/ }));
      }
    };

    it("switches every overlay on and puts the user's choice back at the end", async () => {
      const card = await start();
      expect(walk("r12")).toHaveLength(1);
      expect(walk("g12")).toHaveLength(1);
      expect(walk("bands").length).toBeGreaterThan(0);
      await userEvent.click(within(card).getByRole("button", { name: "End the walkthrough" }));
      expect(walk("r12")).toHaveLength(0);
      expect(walk("bands")).toHaveLength(0);
      expect(chip(/^P\/E bands/)).toHaveAttribute("aria-pressed", "false");
    });

    it("keeps every overlay on while it runs, whatever chip is clicked", async () => {
      await start();
      expect(chip(/^TTM EPS/)).toHaveAttribute("aria-disabled", "true");
      fireEvent.click(chip(/^TTM EPS/));
      expect(walk("eps")).toHaveLength(1);
      expect(chip(/^TTM EPS/)).toHaveAttribute("aria-pressed", "true");
    });

    it("highlights the price line first, with the user's numbers", async () => {
      const card = await start();
      expect(card).toHaveTextContent("step 1 of 10");
      expect(card).toHaveTextContent("Over 3Y the price went from $102.00 to $270.00, +164.7%.");
      for (const e of walk("price")) {
        expect(e).toHaveAttribute("data-thl");
      }
      expect(walk("eps")[0]).not.toHaveAttribute("data-thl");
    });

    it("lifts the band nearest today's P/E on the bands step", async () => {
      const card = await start();
      await next(card, 6);
      expect(card).toHaveTextContent("Use the P/E bands");
      expect(card).toHaveTextContent(
        "is the 25× band, the one closest to today's P/E. Over 3Y the P/E went from 16.5× to 25.0×. Between the first and the last day it moved up past the 17.5×, 20×, 22.5× lines",
      );
      expect(chart().querySelector('[data-bm="25"].bline')).toHaveClass("bh");
      expect(within(panel()).getByTestId("band-caption")).toHaveAttribute("data-thl");
    });

    it("calls today's P/E the one from the user's inputs and rings its levers", async () => {
      await renderReady(
        <>
          <div data-lever="price" />
          <div data-lever="ttmEps" />
          <div data-lever="typicalPeMonths" />
          <div data-lever="targetReturn" />
          <WhenReady>
            <PriceChartPanel />
          </WhenReady>
        </>,
      );
      await userEvent.click(
        within(panel()).getByRole("button", {
          name: "Walk me through the price chart with my numbers",
        }),
      );
      const card = screen.getByRole("region", { name: "Walkthrough of the price chart" });
      await next(card, 5);
      expect(card).toHaveTextContent("Confirm with the P/E line");
      for (const key of ["price", "ttmEps", "typicalPeMonths"]) {
        expect(document.querySelector(`[data-lever="${key}"]`)).toHaveAttribute("data-tside");
      }
      expect(document.querySelector('[data-lever="targetReturn"]')).not.toHaveAttribute(
        "data-tside",
      );
      expect(card).toHaveTextContent(
        "The P/E from your inputs (price ÷ TTM EPS in the left panel) is 25.0×, against its typical 23.0× (the median of the quarter-end P/Es of the last 36 months and the P/E from your inputs): close to its usual level.",
      );
    });

    it("keeps the nearest band lifted on the bands step when another band is hovered", async () => {
      const card = await start();
      await next(card, 6);
      const other = chart().querySelector('[data-bm="20"].bhit') as Element;
      fireEvent.pointerOver(other, { pointerType: "mouse" });
      expect(chart().querySelector('[data-bm="25"].bline')).toHaveClass("bh");
      expect(chart().querySelector('[data-bm="20"].bline')).not.toHaveClass("bh");
      expect(within(panel()).getByTestId("band-caption")).toHaveTextContent("25× band:");
    });

    it("names the share-count events of the range", async () => {
      const card = await start();
      await next(card, 7);
      expect(card).toHaveTextContent(
        "One quarter in this range changed the share count by 2% or more: Q4 FY25 -2.1%.",
      );
    });

    it("ends on the price-earnings link with a take-away", async () => {
      const card = await start();
      await next(card, 9);
      expect(card).toHaveTextContent("step 10 of 10");
      expect(card).toHaveTextContent("Over 3Y the correlation is 0.47, moderate");
      expect(within(panel()).getByText(/price–earnings link: moderate/)).toHaveAttribute(
        "data-thl",
      );
      expect(card.querySelector(".t-takeaway")).toHaveTextContent(
        /^GRMN in one line: over 3Y the price changed \+164\.7% and TTM EPS \+74\.2%.*measured on a log scale, about 57% of the price gain came from higher earnings and 43% from a higher P\/E\.$/,
      );
    });
  });
});
