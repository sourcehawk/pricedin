import { useMemo } from "react";
import type { PriceFeed, SourcedNumber } from "@/api/types";
import type { Levers } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";

/**
 * StepContext holds what a panel's walkthrough lines quote besides the
 * evaluation. The levers are the ones the evaluation was computed from, so
 * a line never mixes the levers of one render with the evaluation of another.
 */
export interface StepContext {
  /** The ticker that the lines name. */
  ticker: string;
  levers: Levers;
  /** The history's price feed and the profile's latest close, which give a quoted close its source. */
  prices: PriceFeed | null;
  latestClose: SourcedNumber | null;
}

/**
 * useStepContext returns the step context of the page's evaluation. It
 * keeps its identity until one of its values changes. Like
 * useEvaluation, it throws outside the page's ready state.
 */
export function useStepContext(): StepContext {
  const { levers, profile, history } = useEvaluation();
  const ticker = profile.ticker.symbol;
  const prices = history.prices;
  const latestClose = profile.latestClose;
  return useMemo(
    () => ({ ticker, levers, prices, latestClose }),
    [ticker, levers, prices, latestClose],
  );
}
