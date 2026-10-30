"use client";

import { EarningsTrajectoryPanel } from "./earnings-trajectory/EarningsTrajectoryPanel";
import { FinancialsPanel } from "./financials/FinancialsPanel";
import { HowDemandingPanel } from "./how-demanding/HowDemandingPanel";
import { PriceChartPanel } from "./price-chart/PriceChartPanel";
import { PricedInPanel } from "./priced-in/PricedInPanel";
import { ReturnGridPanel } from "./return-grid/ReturnGridPanel";
import { ScenariosPanel } from "./scenarios/ScenariosPanel";

/**
 * Panels lays out the panels of the main column in the prototype's order.
 * The page renders it only once an evaluation exists, so each panel can
 * call useEvaluation.
 */
export function Panels() {
  return (
    <>
      <div className="duo">
        <PricedInPanel />
        <PriceChartPanel />
      </div>
      <ReturnGridPanel />
      <div className="two">
        <EarningsTrajectoryPanel />
        <HowDemandingPanel />
      </div>
      <ScenariosPanel />
      <FinancialsPanel />
    </>
  );
}
