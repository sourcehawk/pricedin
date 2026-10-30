import { act, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { exampleEvaluation } from "@/api/testing/fixtures";
import { TipProvider } from "@/components/tip/TipProvider";
import type { Chart } from "@/engine";
import { createHoverStore } from "./hover";
import { PricePlot, type Marker } from "./PricePlot";

const chart = exampleEvaluation.priceChart.value as Chart;
const day = chart.points[Math.floor(chart.points.length / 2)]?.date ?? chart.end;

function marker(from: number, to: number): Marker {
  return {
    event: { label: "Q1 FY26", periodEnd: day, from, to, change: to / from - 1, kind: "buyback" },
    href: null,
    sources: { from: null, to: null },
  };
}

function plot(m: Marker) {
  return (
    <TipProvider>
      <PricePlot
        chart={chart}
        ticker="GRMN"
        shown={new Set(["sh"])}
        lifted={null}
        onBand={() => undefined}
        hover={createHoverStore()}
        markers={[m]}
        feed={null}
        latest={null}
        quarters={[]}
      />
    </TipProvider>
  );
}

describe("PricePlot share-count marker", () => {
  it("shows the new counts in an open tooltip after the history changes", () => {
    const { rerender } = render(plot(marker(200_000_000, 190_000_000)));
    act(() => {
      screen.getByRole("img", { name: /^Q1 FY26: buyback/ }).focus();
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("200.0M → 190.0M");
    rerender(plot(marker(200_000_000, 180_000_000)));
    expect(screen.getByRole("tooltip")).toHaveTextContent("200.0M → 180.0M");
  });

  it("closes its tooltip when the marker leaves the chart", () => {
    const { rerender } = render(plot(marker(200_000_000, 190_000_000)));
    act(() => {
      screen.getByRole("img", { name: /^Q1 FY26: buyback/ }).focus();
    });
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
    rerender(
      <TipProvider>
        <PricePlot
          chart={chart}
          ticker="GRMN"
          shown={new Set(["sh"])}
          lifted={null}
          onBand={() => undefined}
          hover={createHoverStore()}
          markers={[]}
          feed={null}
          latest={null}
          quarters={[]}
        />
      </TipProvider>,
    );
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
});
