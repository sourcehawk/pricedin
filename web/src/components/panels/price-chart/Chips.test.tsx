import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { exampleEvaluation, historyFixture } from "@/api/testing/fixtures";
import { TipProvider } from "@/components/tip/TipProvider";
import type { Chart } from "@/engine";
import { Chips } from "./Chips";
import { createHoverStore } from "./hover";
import { closeSourced } from "./sources";

const points = (exampleEvaluation.priceChart.value as Chart).points;
const sourceOf = (date: string, close: number) =>
  closeSourced(date, close, historyFixture.prices, null);

function chips(shown: typeof points, hover = createHoverStore()) {
  return (
    <TipProvider>
      <Chips
        points={shown}
        hover={hover}
        shown={new Set()}
        locked={false}
        onToggle={() => undefined}
        sourceOf={sourceOf}
      />
    </TipProvider>
  );
}

describe("Chips", () => {
  it("keeps an open tooltip of the price chip on the close that the chip shows", async () => {
    const { rerender } = render(chips(points.slice(0, -1)));
    const before = points.at(-2)?.date ?? "?";
    const group = screen.getByRole("group", { name: "Lines on the price chart" });
    within(group).getByText("Price").parentElement?.focus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(`As of close ${before}`);

    rerender(chips(points));
    const after = points.at(-1)?.date ?? "?";
    expect(screen.getByRole("tooltip")).toHaveTextContent(`As of close ${after}`);
  });
});
