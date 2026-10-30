import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { WalkthroughSlot } from "@/components/walkthrough/WalkthroughProvider";
import { leversReducer } from "@/state/levers";
import { renderReady, testLevers } from "@/testing/render";
import { Rail } from "./Rail";

withApiServer();

async function startTour() {
  await renderReady(
    <div className="grid">
      <Rail />
      <main>
        <WalkthroughSlot name="main" />
      </main>
    </div>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Walk me through these inputs" }));
  return screen.getByRole("region", { name: "Walkthrough of the inputs" });
}

describe("the walkthrough of the inputs", () => {
  it("writes its card at the top of the main column", async () => {
    const card = await startTour();
    expect(screen.getByRole("main")).toContainElement(card);
    expect(card).toHaveTextContent("step 1 of 7");
  });

  it("rings the lever of each step", async () => {
    await startTour();
    const lever = (key: string) => document.querySelector(`[data-lever="${key}"]`);
    expect(lever("targetReturn")).toHaveClass("tside");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(lever("targetReturn")).not.toHaveClass("tside");
    expect(lever("holdingYears")).toHaveClass("tside");
  });

  it("uses the user's own numbers", async () => {
    const card = await startTour();
    expect(card).toHaveTextContent(
      /Your target is 15\.0%, above the S&P 500's long-run 10%\. At 15\.0% GRMN requires \d+\.\d% growth; at 10% it would require \d+\.\d%\./,
    );
  });

  it("shows n/a, not 0.0×, for a P/E that the engine cannot compute", async () => {
    await renderReady(
      <div className="grid">
        <Rail />
        <main>
          <WalkthroughSlot name="main" />
        </main>
      </div>,
      {
        levers: leversReducer(testLevers(), {
          type: "setStartingPoint",
          field: "ttmEps",
          value: -1,
        }),
      },
    );
    await userEvent.click(screen.getByRole("button", { name: "Walk me through these inputs" }));
    const card = screen.getByRole("region", { name: "Walkthrough of the inputs" });
    await userEvent.click(within(card).getByRole("button", { name: "Next" }));
    expect(card).toHaveTextContent("With a n/a exit");
    await userEvent.click(within(card).getByRole("button", { name: "Next" }));
    expect(card).toHaveTextContent("GRMN trades at n/a today");
    expect(card).not.toHaveTextContent("0.0×");
  });

  it("ends with a one-line take-away", async () => {
    const card = await startTour();
    for (let i = 0; i < 6; i++) {
      await userEvent.click(within(card).getByRole("button", { name: "Next" }));
    }
    expect(card).toHaveTextContent(/GRMN in one line: for 15\.0% a year over 3 years/);
    expect(document.querySelector('[data-lever="dividend"]')).toHaveClass("tside");
  });
});
