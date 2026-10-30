import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { InfoTip } from "./InfoTip";
import { TipProvider, useTip, useTipHandlers } from "./TipProvider";

/** Elsewhere is another element with a tooltip of its own, which a click hides. */
function Elsewhere() {
  const tip = useTip();
  return (
    <button
      type="button"
      onClick={(e) => {
        tip.hide(e.currentTarget);
      }}
    >
      Elsewhere
    </button>
  );
}

function tap(el: HTMLElement) {
  fireEvent.pointerDown(el, { pointerType: "touch" });
  act(() => {
    el.focus();
  });
  fireEvent.click(el);
}

describe("InfoTip", () => {
  it("shows its explanation on the first tap, where a tap also focuses the button", () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
      </TipProvider>,
    );
    tap(screen.getByRole("button", { name: "About P/E" }));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Share price divided by TTM EPS.");
  });

  it("hides it on the second tap", () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
      </TipProvider>,
    );
    const button = screen.getByRole("button", { name: "About P/E" });
    tap(button);
    tap(button);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("shows it on keyboard focus", () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
      </TipProvider>,
    );
    act(() => {
      screen.getByRole("button", { name: "About P/E" }).focus();
    });
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
  });

  it("hides a tapped tooltip when the page or a panel scrolls", () => {
    render(
      <TipProvider>
        <div data-testid="panel" style={{ overflow: "auto" }}>
          <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
        </div>
      </TipProvider>,
    );
    tap(screen.getByRole("button", { name: "About P/E" }));
    fireEvent.scroll(screen.getByTestId("panel"));
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("stays open while the pointer moves from the button onto the tooltip", async () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
      </TipProvider>,
    );
    const button = screen.getByRole("button", { name: "About P/E" });
    fireEvent.pointerEnter(button, { pointerType: "mouse", clientX: 10, clientY: 10 });
    fireEvent.pointerLeave(button, { pointerType: "mouse" });
    fireEvent.pointerEnter(screen.getByRole("tooltip"), { pointerType: "mouse" });
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
    fireEvent.pointerLeave(screen.getByRole("tooltip"), { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    });
  });

  it("closes after the pointer leaves the button for somewhere else", async () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
      </TipProvider>,
    );
    const button = screen.getByRole("button", { name: "About P/E" });
    fireEvent.pointerEnter(button, { pointerType: "mouse", clientX: 10, clientY: 10 });
    fireEvent.pointerLeave(button, { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    });
  });

  it("closes after the pointer leaves, also when another element hides its own tooltip meanwhile", async () => {
    render(
      <TipProvider>
        <InfoTip label="About P/E">Share price divided by TTM EPS.</InfoTip>
        <Elsewhere />
      </TipProvider>,
    );
    const button = screen.getByRole("button", { name: "About P/E" });
    fireEvent.pointerEnter(button, { pointerType: "mouse", clientX: 10, clientY: 10 });
    fireEvent.pointerLeave(button, { pointerType: "mouse" });
    fireEvent.click(screen.getByRole("button", { name: "Elsewhere" }));
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    });
  });
});

describe("useTipHandlers", () => {
  function Item({ data, content }: { data: number; content: () => string }) {
    const handlers = useTipHandlers(content, data);
    return (
      <button type="button" {...handlers}>
        item
      </button>
    );
  }

  it("computes no content for a tooltip that is not open", () => {
    let calls = 0;
    const content = () => {
      calls++;
      return "tip";
    };
    const { rerender } = render(
      <TipProvider>
        <Item data={1} content={content} />
      </TipProvider>,
    );
    rerender(
      <TipProvider>
        <Item data={2} content={content} />
      </TipProvider>,
    );
    expect(calls).toBe(0);
  });

  it("shows new content in an open tooltip when its data changes", () => {
    const { rerender } = render(
      <TipProvider>
        <Item data={1} content={() => "one"} />
      </TipProvider>,
    );
    act(() => {
      screen.getByRole("button", { name: "item" }).focus();
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("one");
    rerender(
      <TipProvider>
        <Item data={2} content={() => "two"} />
      </TipProvider>,
    );
    expect(screen.getByRole("tooltip")).toHaveTextContent("two");
  });
});
