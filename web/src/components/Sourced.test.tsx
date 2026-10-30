import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { SecSource, SourcedNumber } from "@/api/types";
import { priceSourced, ttmEpsSourced } from "@/api/testing/fixtures";
import { Sourced } from "./Sourced";
import { TipProvider } from "./tip/TipProvider";

function renderSourced(ui: React.ReactElement) {
  return render(<TipProvider>{ui}</TipProvider>);
}

const tenQ: SourcedNumber = {
  value: 2.75,
  source: {
    kind: "sec",
    form: "10-Q",
    accession: "0001045810-26-000075",
    filed: "2026-08-26",
    concept: "EarningsPerShareDiluted",
    periodStart: "2026-04-28",
    periodEnd: "2026-07-26",
    reported: 2.75,
    splitRatio: 1,
    scale: 1,
    url: "https://www.sec.gov/Archives/edgar/data/1045810/000104581026000075/",
  },
  asOf: "2026-08-26",
};

describe("Sourced", () => {
  it("shows the provider and the close date of a price on hover", async () => {
    renderSourced(<Sourced value={priceSourced}>$270.00</Sourced>);
    await userEvent.hover(screen.getByText("$270.00"));
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent(/massive/i);
    expect(tip).toHaveTextContent(`As of close ${priceSourced.asOf}`);
  });

  it("shows the form, the filing date and the period of a filed value on focus", async () => {
    renderSourced(<Sourced value={tenQ}>$2.75</Sourced>);
    await userEvent.tab();
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Form 10-Q");
    expect(tip).toHaveTextContent("filed 2026-08-26");
    expect(tip).toHaveTextContent("period ending 2026-07-26");
    expect(screen.getByText("$2.75").closest("a")).toHaveAttribute(
      "aria-describedby",
      "pricedin-tip",
    );
  });

  it("links a filed value to its filing", () => {
    renderSourced(<Sourced value={tenQ}>$2.75</Sourced>);
    expect(screen.getByRole("link", { name: "$2.75" })).toHaveAttribute(
      "href",
      tenQ.source.kind === "sec" ? tenQ.source.url : "",
    );
  });

  it("lists the filings that a TTM value adds up and links the latest one", async () => {
    renderSourced(<Sourced value={ttmEpsSourced}>$10.80</Sourced>);
    await userEvent.hover(screen.getByText("$10.80"));
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent(/last four quarters/i);
    expect(tip.querySelectorAll("li")).toHaveLength(4);
    const inputs = ttmEpsSourced.source.kind === "derived" ? ttmEpsSourced.source.inputs : [];
    const latest = inputs.at(-1);
    expect(screen.getByRole("link", { name: "$10.80" })).toHaveAttribute(
      "href",
      latest?.kind === "sec" ? latest.url : "",
    );
  });

  it("links the newest filing also when it sits in a nested derived input", () => {
    const sec = (form: string, filed: string): SecSource => ({
      kind: "sec",
      form,
      accession: `acc-${filed}`,
      filed,
      concept: "EarningsPerShareDiluted",
      periodStart: null,
      periodEnd: filed,
      reported: 1,
      splitRatio: 1,
      scale: 1,
      url: `https://www.sec.gov/${form}/${filed}`,
    });
    const ttm: SourcedNumber = {
      value: 4,
      source: {
        kind: "derived",
        method: "ttm",
        estimate: false,
        inputs: [
          sec("10-Q", "2026-05-01"),
          sec("10-Q", "2026-08-01"),
          sec("10-Q", "2026-11-01"),
          {
            kind: "derived",
            method: "fy_difference",
            estimate: false,
            inputs: [sec("10-K", "2027-02-20"), sec("10-Q", "2026-11-01")],
          },
        ],
      },
      asOf: "2027-02-20",
    };
    renderSourced(<Sourced value={ttm}>$4.00</Sourced>);
    expect(screen.getByRole("link", { name: "$4.00" })).toHaveAttribute(
      "href",
      "https://www.sec.gov/10-K/2027-02-20",
    );
  });

  it("says that the user typed a value", async () => {
    renderSourced(
      <Sourced value={null} user>
        $250.00
      </Sourced>,
    );
    await userEvent.hover(screen.getByText("$250.00"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("You typed this");
  });

  it("hides the tooltip on Escape", async () => {
    renderSourced(<Sourced value={priceSourced}>$270.00</Sourced>);
    await userEvent.hover(screen.getByText("$270.00"));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("shows only the number when it has no source", () => {
    renderSourced(<Sourced value={null}>n/a</Sourced>);
    expect(screen.getByText("n/a").tagName).toBe("SPAN");
    expect(screen.getByText("n/a")).not.toHaveAttribute("tabindex");
  });
});
