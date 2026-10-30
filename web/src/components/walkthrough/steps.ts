import { useMemo } from "react";
import type { Levers } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";
import type { WalkthroughStep } from "./Walkthrough";

/**
 * StepContext holds what a panel's walkthrough lines quote besides the
 * evaluation. The levers are the ones the evaluation was computed from, so
 * a line never mixes the levers of one render with the evaluation of another.
 */
export interface StepContext {
  /** The ticker that the lines name. */
  ticker: string;
  levers: Levers;
}

/** PanelSteps is the shape of each panel's steps function, exported from the panel's steps.tsx. */
export type PanelSteps = (ctx: StepContext) => WalkthroughStep[];

/**
 * useStepContext returns the step context of the page's evaluation. It
 * keeps its identity until the ticker or the levers change. Like
 * useEvaluation, it throws outside the page's ready state.
 */
export function useStepContext(): StepContext {
  const { levers, profile } = useEvaluation();
  const ticker = profile.ticker.symbol;
  return useMemo(() => ({ ticker, levers }), [ticker, levers]);
}
