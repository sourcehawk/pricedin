import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { filedOn, financialsHistory, serveHistory, renderPanel } from "./testing";

withApiServer();

beforeEach(() => {
  serveHistory(financialsHistory());
});

async function openCompare() {
  const r = await renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Compare" }));
  return r;
}

function chip(name: string): HTMLElement {
  return within(screen.getByRole("group", { name: "Metrics to compare" })).getByRole("button", {
    name,
  });
}

function table(): HTMLElement {
  return screen.getByRole("table", { name: /Do they move together from quarter to quarter/ });
}

describe("CompareView", () => {
  it("starts with revenue, operating income, EPS and the share count", async () => {
    await openCompare();
    expect(chip("Revenue")).toHaveAttribute("aria-pressed", "true");
    expect(chip("Operating costs")).toHaveAttribute("aria-pressed", "false");
    expect(chip("Operating income")).toHaveAttribute("aria-pressed", "true");
    expect(chip("Diluted EPS")).toHaveAttribute("aria-pressed", "true");
    expect(chip("Free cash flow")).toHaveAttribute("aria-pressed", "false");
    expect(chip("Diluted shares")).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByRole("group", { name: /Financial metrics compared by quarter for GRMN/ }),
    ).toBeInTheDocument();
  });

  it("shows each metric's change since the first quarter by default", async () => {
    const { container } = await openCompare();
    expect(screen.getByRole("button", { name: "Since Q4'24" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.pointerEnter(container.querySelectorAll("rect.cmpcol")[7] as Element, {
      pointerType: "mouse",
      clientX: 5,
      clientY: 5,
    });
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Diluted EPS+12%$2.75");
    expect(tip).toHaveTextContent("Total change since Q4 FY24");
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
  });

  it("lets the keyboard reach each quarter's values", async () => {
    const { container } = await openCompare();
    const chart = screen.getByRole("group", {
      name: /Financial metrics compared by quarter for GRMN/,
    });
    const quarter = within(chart).getByRole("img", {
      name: "Q3 FY26: revenue +9% ($1.99B), operating income +18% ($612M), diluted EPS +12% ($2.75), diluted shares -2% (192.0M)",
    });
    act(() => {
      quarter.focus();
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Diluted EPS+12%$2.75");
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
    act(() => {
      quarter.blur();
    });
    expect(container.querySelector("line.col-guide")).not.toBeInTheDocument();
  });

  it("switches to the change from the quarter before", async () => {
    const { container } = await openCompare();
    await userEvent.click(screen.getByRole("button", { name: "Each quarter" }));
    expect(screen.getByRole("button", { name: "Each quarter" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.pointerEnter(container.querySelectorAll("rect.cmpcol")[7] as Element, {
      pointerType: "mouse",
      clientX: 5,
      clientY: 5,
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Diluted EPS+6%$2.75");
  });

  it("adds and removes a metric with its chip", async () => {
    await openCompare();
    await userEvent.click(chip("Free cash flow"));
    expect(chip("Free cash flow")).toHaveAttribute("aria-pressed", "true");
    expect(
      within(table()).getByRole("columnheader", { name: "Free cash flow" }),
    ).toBeInTheDocument();
    await userEvent.click(chip("Diluted shares"));
    expect(
      within(table()).queryByRole("columnheader", { name: "Diluted shares" }),
    ).not.toBeInTheDocument();
  });

  it("shows the correlation of each pair of selected metrics", async () => {
    await openCompare();
    const t = table();
    const row = within(t).getByRole("rowheader", { name: "Revenue" }).closest("tr") as HTMLElement;
    const cells = within(row).getAllByRole("cell");
    expect(cells.map((c) => c.textContent)).toEqual(["·", "0.97", "0.94", "-0.56"]);
  });

  it("explains a correlation on focus", async () => {
    await openCompare();
    const cell = within(table()).getAllByText("0.97")[0] as HTMLElement;
    act(() => {
      cell.focus();
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "Revenue and Operating incomeCorrelation 0.97: strong, same direction.",
    );
  });

  it("says that a correlation rests on at most the quarter-to-quarter changes of the view", async () => {
    await openCompare();
    act(() => {
      screen.getByRole("button", { name: "About this table" }).focus();
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "It uses at most 7 quarter-to-quarter changes, so read it as a hint.",
    );
  });

  it("names the filing of each amount in the quarter's tooltip", async () => {
    const { container } = await openCompare();
    fireEvent.pointerEnter(container.querySelectorAll("rect.cmpcol")[7] as Element, {
      pointerType: "mouse",
      clientX: 5,
      clientY: 5,
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      `Diluted EPS+12%$2.75 · Form 10-Q filed ${filedOn("2026-09-30")}`,
    );
  });

  it("keeps a tapped quarter's tooltip open until the next tap", async () => {
    const { container } = await openCompare();
    const col = container.querySelectorAll("rect.cmpcol")[7] as Element;
    fireEvent.pointerDown(col, { pointerType: "touch" });
    fireEvent.pointerEnter(col, { pointerType: "touch" });
    fireEvent.pointerLeave(col, { pointerType: "touch" });
    fireEvent.click(col);
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Q3 FY26");
    expect(container.querySelector("line.col-guide")).toBeInTheDocument();
    fireEvent.pointerDown(col, { pointerType: "touch" });
    fireEvent.click(col);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(container.querySelector("line.col-guide")).not.toBeInTheDocument();
  });

  it("hides the table with fewer than two metrics and asks for one with none", async () => {
    await openCompare();
    await userEvent.click(chip("Operating income"));
    await userEvent.click(chip("Diluted EPS"));
    await userEvent.click(chip("Diluted shares"));
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    await userEvent.click(chip("Revenue"));
    expect(screen.getByText("Pick at least one metric.")).toBeInTheDocument();
  });

  it("keeps the tiles under the compare chart", async () => {
    await openCompare();
    expect(screen.getByText("Revenue Q3 FY26")).toBeInTheDocument();
  });
});
