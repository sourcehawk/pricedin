import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { withApiServer } from "@/api/testing/server";
import { pct } from "@/components/format";
import { renderReady } from "@/testing/render";
import { Walkthrough, type WalkthroughStep } from "./Walkthrough";
import { WalkthroughSlot } from "./WalkthroughProvider";

withApiServer();

const steps: WalkthroughStep[] = [
  {
    title: "The big number",
    body: "The bar to clear.",
    highlight: ["big"],
    levers: ["targetReturn"],
    you: (ev) => `You need ${pct(ev.required.growth.value)} a year.`,
  },
  { title: "The gap", body: "Baseline minus requirement.", highlight: ["gap"] },
  {
    title: "Move the levers",
    body: "Try a lower exit P/E.",
    levers: ["exitPe"],
    takeaway: (ev) => `In one line: ${pct(ev.required.growth.value)}.`,
  },
];

function Panel({ id = "demo", slot }: { id?: string; slot?: string }) {
  return (
    <section data-walk-scope aria-label={`panel ${id}`}>
      <div data-walk="big">
        <b>14.8%</b>
      </div>
      <p data-walk="gap">-2.0 pp</p>
      <p>Other text</p>
      <Walkthrough
        id={id}
        name="the demo"
        launcher="Walk me through the demo"
        steps={steps}
        intro="This panel answers one question."
        watchOut="Rules of thumb."
        {...(slot === undefined ? {} : { slot })}
      />
    </section>
  );
}

function Rail() {
  return (
    <aside>
      <div data-lever="targetReturn">Target</div>
      <div data-lever="exitPe">Exit P/E</div>
      <input aria-label="typing here" />
    </aside>
  );
}

const scrollIntoView = vi.fn();
const scrollTo = vi.fn();
beforeEach(() => {
  Element.prototype.scrollIntoView = scrollIntoView;
  window.scrollTo = scrollTo;
});
afterEach(() => {
  scrollIntoView.mockReset();
  scrollTo.mockReset();
});

async function start(name = "Walk me through the demo") {
  await userEvent.click(screen.getByRole("button", { name }));
}

const card = () => screen.getByRole("region", { name: "Walkthrough of the demo" });

describe("Walkthrough", () => {
  it("writes the first step into the panel, not into a modal", async () => {
    await renderReady(
      <>
        <Rail />
        <Panel />
      </>,
    );
    await start();
    const panel = screen.getByRole("region", { name: "panel demo" });
    expect(within(panel).getByRole("region", { name: "Walkthrough of the demo" })).toBeVisible();
    expect(card()).toHaveTextContent("step 1 of 3");
    expect(card()).toHaveTextContent("The big number");
    expect(card()).toHaveTextContent("This panel answers one question.");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.querySelector("dialog")).toBeNull();
  });

  it("adds a line with the user's own numbers", async () => {
    await renderReady(<Panel />);
    await start();
    expect(within(card()).getByText(/^You need \d+\.\d% a year\.$/)).toBeInTheDocument();
  });

  it("highlights what the step explains and fades the rest of the panel", async () => {
    await renderReady(<Panel />);
    await start();
    const panel = screen.getByRole("region", { name: "panel demo" });
    expect(panel).toHaveAttribute("data-tour-on");
    expect(panel.querySelector('[data-walk="big"]')).toHaveAttribute("data-thl");
    expect(screen.getByText("Other text")).toHaveAttribute("data-tl");
    expect(within(card()).getByText("The big number")).not.toHaveAttribute("data-tl");
  });

  it("rings the lever a step refers to and moves the ring with the steps", async () => {
    await renderReady(
      <>
        <Rail />
        <Panel />
      </>,
    );
    await start();
    expect(screen.getByText("Target")).toHaveAttribute("data-tside");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Target")).not.toHaveAttribute("data-tside");
    expect(screen.getByText("-2.0 pp")).toHaveAttribute("data-thl");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Exit P/E")).toHaveAttribute("data-tside");
  });

  it("shows the take-away and the watch-out note on the last step and ends on Finish", async () => {
    await renderReady(<Panel />);
    await start();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(card()).toHaveTextContent(/In one line: \d+\.\d%\./);
    expect(card()).toHaveTextContent("Watch out. Rules of thumb.");
    await userEvent.click(screen.getByRole("button", { name: "Finish" }));
    expect(screen.queryByRole("region", { name: "Walkthrough of the demo" })).toBeNull();
    expect(screen.getByRole("button", { name: "Walk me through the demo" })).toHaveFocus();
  });

  it("goes back a step and cannot go back from the first", async () => {
    await renderReady(<Panel />);
    await start();
    expect(screen.getByRole("button", { name: "Back" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(card()).toHaveTextContent("step 1 of 3");
  });

  it("moves with the arrow keys and ends on Escape", async () => {
    await renderReady(<Panel />);
    await start();
    await userEvent.keyboard("{ArrowRight}");
    expect(card()).toHaveTextContent("step 2 of 3");
    await userEvent.keyboard("{ArrowLeft}");
    expect(card()).toHaveTextContent("step 1 of 3");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("region", { name: "Walkthrough of the demo" })).toBeNull();
  });

  it("leaves the arrow keys to a text field", async () => {
    await renderReady(
      <>
        <Rail />
        <Panel />
      </>,
    );
    await start();
    await userEvent.click(screen.getByRole("textbox", { name: "typing here" }));
    await userEvent.keyboard("{ArrowRight}");
    expect(card()).toHaveTextContent("step 1 of 3");
  });

  it("removes every mark when it ends", async () => {
    await renderReady(
      <>
        <Rail />
        <Panel />
      </>,
    );
    await start();
    await userEvent.click(screen.getByRole("button", { name: "End the walkthrough" }));
    expect(
      document.querySelector("[data-tour-on], [data-thl], [data-tside], [data-tl]"),
    ).toBeNull();
  });

  it("keeps a highlight when a component in the panel re-renders its element from its own state", async () => {
    function Toggle() {
      const [on, setOn] = useState(false);
      return (
        <>
          <span data-walk="big" className={on ? "on" : "off"}>
            14.8%
          </span>
          <button
            type="button"
            onClick={() => {
              setOn(!on);
            }}
          >
            toggle
          </button>
        </>
      );
    }
    await renderReady(
      <section data-walk-scope>
        <Toggle />
        <Walkthrough id="t" name="the demo" launcher="Go" steps={steps} />
      </section>,
    );
    await start("Go");
    await userEvent.click(screen.getByRole("button", { name: "toggle" }));
    expect(screen.getByText("14.8%")).toHaveClass("on");
    expect(screen.getByText("14.8%")).toHaveAttribute("data-thl");
  });

  it("marks a node that a component in the panel mounts during a step", async () => {
    function More() {
      const [more, setMore] = useState(false);
      return (
        <>
          <button
            type="button"
            onClick={() => {
              setMore(true);
            }}
          >
            more
          </button>
          {more && (
            <>
              <b data-walk="big">20%</b>
              <i>new text</i>
            </>
          )}
          <p data-walk="big">14.8%</p>
        </>
      );
    }
    await renderReady(
      <section data-walk-scope>
        <More />
        <Walkthrough id="m" name="the demo" launcher="Go" steps={steps} />
      </section>,
    );
    await start("Go");
    await userEvent.click(screen.getByRole("button", { name: "more" }));
    await waitFor(() => {
      expect(screen.getByText("20%")).toHaveAttribute("data-thl");
    });
    expect(screen.getByText("new text")).toHaveAttribute("data-tl");
  });

  it("never scrolls the page", async () => {
    await renderReady(
      <>
        <Rail />
        <Panel />
      </>,
    );
    await start();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await userEvent.keyboard("{ArrowRight}{Escape}");
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).not.toHaveBeenCalled();
  });

  it("runs one walkthrough at a time", async () => {
    await renderReady(
      <>
        <Panel id="one" />
        <Panel id="two" />
      </>,
    );
    const [first, second] = screen.getAllByRole("button", { name: "Walk me through the demo" });
    await userEvent.click(first as HTMLElement);
    expect(screen.getAllByRole("region", { name: "Walkthrough of the demo" })).toHaveLength(1);
    await userEvent.click(second as HTMLElement);
    const cards = screen.getAllByRole("region", { name: "Walkthrough of the demo" });
    expect(cards).toHaveLength(1);
    expect(screen.getByRole("region", { name: "panel two" })).toContainElement(cards[0] ?? null);
  });

  it("leaves the focus in the new card when a walkthrough earlier on the page starts", async () => {
    await renderReady(
      <>
        <Panel id="one" />
        <Panel id="two" />
      </>,
    );
    const [first, second] = screen.getAllByRole("button", { name: "Walk me through the demo" });
    await userEvent.click(second as HTMLElement);
    await userEvent.click(first as HTMLElement);
    const panel = screen.getByRole("region", { name: "panel one" });
    expect(within(panel).getByRole("button", { name: "Next" })).toHaveFocus();
  });

  it("writes the card into its slot when it has one", async () => {
    await renderReady(
      <>
        <main aria-label="main column">
          <WalkthroughSlot name="main" />
        </main>
        <Panel slot="main" />
      </>,
    );
    await start();
    expect(screen.getByRole("main", { name: "main column" })).toContainElement(card());
  });

  it("runs the setup of onStart and its cleanup at the end", async () => {
    const cleanup = vi.fn();
    const setup = vi.fn(() => cleanup);
    await renderReady(
      <section data-walk-scope>
        <Walkthrough id="x" name="x" launcher="Go" steps={steps} onStart={setup} />
      </section>,
    );
    await start("Go");
    expect(setup).toHaveBeenCalledTimes(1);
    await userEvent.keyboard("{Escape}");
    expect(cleanup).toHaveBeenCalledTimes(1);
  });
});
