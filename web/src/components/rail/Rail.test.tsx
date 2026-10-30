import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { renderReady } from "@/testing/render";
import { Rail } from "./Rail";

withApiServer();

const field = (name: string | RegExp) => screen.getByRole("spinbutton", { name });
const slider = (name: string | RegExp) => screen.getByRole("slider", { name });
const pressed = (group: string) =>
  within(screen.getByRole("group", { name: group }))
    .getAllByRole("button")
    .filter((b) => b.getAttribute("aria-pressed") === "true")
    .map((b) => b.textContent);

async function replace(el: HTMLElement, text: string) {
  await userEvent.clear(el);
  await userEvent.type(el, text);
}

describe("target return", () => {
  it("moves the slider when the user types a value", async () => {
    await renderReady(<Rail />);
    await replace(field("Target return, percent a year"), "12");
    expect(slider("Target return")).toHaveValue("12");
  });

  it("updates the text field when the slider moves", async () => {
    await renderReady(<Rail />);
    fireEvent.change(slider("Target return"), { target: { value: "8.5" } });
    expect(field("Target return, percent a year")).toHaveValue(8.5);
  });

  it("shows the clamped value after a value above 50%", async () => {
    await renderReady(<Rail />);
    const f = field("Target return, percent a year");
    await replace(f, "80");
    await userEvent.tab();
    expect(f).toHaveValue(50);
  });
});

describe("holding period", () => {
  it("keeps the text field, the slider and the year buttons in sync", async () => {
    await renderReady(<Rail />);
    await userEvent.click(screen.getByRole("button", { name: "5Y" }));
    expect(field("Holding period, years")).toHaveValue(5);
    expect(slider("Holding period")).toHaveValue("5");
    await replace(field("Holding period, years"), "10");
    expect(pressed("Holding period")).toEqual(["10Y"]);
  });

  it("switches with the keyboard", async () => {
    await renderReady(<Rail />);
    screen.getByRole("button", { name: "1Y" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(field("Holding period, years")).toHaveValue(1);
    expect(
      screen.getByText("year", { exact: false, selector: ".numfield span" }),
    ).toHaveTextContent("year");
  });
});

describe("exit P/E", () => {
  it("switches to Custom when the user types a value", async () => {
    await renderReady(<Rail />);
    expect(pressed("Exit P/E")).toEqual(["Estimated"]);
    await replace(field("Exit P/E multiple, type a number"), "30");
    expect(pressed("Exit P/E")).toEqual(["Custom"]);
    expect(slider("Exit P/E multiple")).toHaveValue("30");
  });

  it("starts Custom from the exit P/E in use", async () => {
    await renderReady(<Rail />);
    const before = (field("Exit P/E multiple, type a number") as HTMLInputElement).value;
    await userEvent.click(
      within(screen.getByRole("group", { name: "Exit P/E" })).getByRole("button", {
        name: "Custom",
      }),
    );
    expect(field("Exit P/E multiple, type a number")).toHaveValue(Number(before));
    expect(screen.getByText(/^Your own number\./)).toBeInTheDocument();
  });

  it("says in its tooltip that the typical P/E months change without picking Trailing", async () => {
    await renderReady(<Rail />);
    await userEvent.hover(screen.getByRole("button", { name: "About the exit P/E" }));
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "The typical P/E line under the slider shows that level, and you can change its months there without picking Trailing.",
    );
  });

  it("explains the estimate under the lever", async () => {
    await renderReady(<Rail />);
    expect(screen.getByText(/^Today the market pays 25\.0× earnings\./)).toBeInTheDocument();
  });
});

describe("growth baseline", () => {
  it("switches to a flat custom rate when the user types a growth rate", async () => {
    await renderReady(<Rail />);
    await replace(field(/^Growth baseline, percent a year/), "9");
    expect(pressed("Growth baseline")).toEqual(["Custom"]);
    expect(pressed("Custom growth")).toEqual(["Same each year"]);
    expect(slider("Custom EPS growth")).toHaveValue("9");
  });

  it("follows the trend year by year from the trajectory", async () => {
    await renderReady(<Rail />);
    await userEvent.click(screen.getByRole("button", { name: "Follow this trend year by year" }));
    expect(pressed("Custom growth")).toEqual(["Year by year"]);
    expect(screen.getAllByRole("spinbutton", { name: /^EPS growth in year/ })).toHaveLength(3);
    expect(screen.getByText(/^Filled from the weighted trend’s trajectory/)).toBeInTheDocument();
    await replace(field("EPS growth in year 2, percent"), "5");
    expect(screen.getByText("Your own rates.")).toBeInTheDocument();
  });
});

describe("starting point", () => {
  it("is filled from the stock and says so", async () => {
    await renderReady(<Rail />);
    expect(field("Share price ($)")).toHaveValue(270);
    expect(field("TTM EPS ($)")).toHaveValue(10.8);
    expect(screen.getByText(/^Filled from GRMN/)).toBeInTheDocument();
  });

  it("says Edited by you after the user types a price", async () => {
    await renderReady(<Rail />);
    await replace(field("Share price ($)"), "250");
    expect(screen.getByText(/^Edited by you\. GRMN closed at/)).toBeInTheDocument();
  });

  it("goes back to the stock's value when the field is emptied", async () => {
    await renderReady(<Rail />);
    await replace(field("Share price ($)"), "250");
    await userEvent.clear(field("Share price ($)"));
    await userEvent.tab();
    expect(field("Share price ($)")).toHaveValue(270);
  });
});

describe("Reset inputs", () => {
  it("puts every input back, the target return and holding period included", async () => {
    await renderReady(<Rail />);
    await replace(field("Target return, percent a year"), "10");
    await userEvent.click(screen.getByRole("button", { name: "5Y" }));
    await replace(field("Share price ($)"), "250");
    await userEvent.click(screen.getByRole("button", { name: "Reset inputs" }));
    await waitFor(() => {
      expect(field("Target return, percent a year")).toHaveValue(15);
    });
    expect(field("Holding period, years")).toHaveValue(3);
    expect(field("Share price ($)")).toHaveValue(270);
  });
});

describe("lever keys for the walkthrough rings", () => {
  const lever = (key: string) => document.querySelector(`[data-lever="${key}"]`);

  it("mark every lever and both look-backs", async () => {
    await renderReady(<Rail />);
    for (const key of [
      "price",
      "ttmEps",
      "dividend",
      "targetReturn",
      "holdingYears",
      "exitPe",
      "typicalPeMonths",
      "baseline",
      "trailingMonths",
    ]) {
      expect(lever(key)).not.toBeNull();
    }
    expect(lever("typicalPeMonths")).toHaveTextContent("Typical P/E");
    expect(lever("trailingMonths")).toHaveTextContent("Trailing growth");
  });

  it("mark the flat custom rate and the year-by-year rates when they show", async () => {
    await renderReady(<Rail />);
    await replace(field(/^Growth baseline, percent a year/), "9");
    expect(lever("customGrowth")).toContainElement(slider("Custom EPS growth"));
    await userEvent.click(screen.getByRole("button", { name: "Year by year" }));
    expect(lever("yearRates")).toContainElement(field("EPS growth in year 1, percent"));
  });
});
