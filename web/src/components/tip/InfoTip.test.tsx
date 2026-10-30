import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { InfoTip } from "./InfoTip";
import { TipProvider } from "./TipProvider";

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
});
