import { useEffect, type ReactNode } from "react";
import type { Evaluation } from "@/engine";
import { leversReducer, type LeverAction, type LeverState } from "@/state/levers";
import { useStock } from "@/state/StockProvider";
import { testLevers } from "@/testing/render";

/** evaluationProbe returns a component that records the evaluation the page shows, and a getter for it. */
export function evaluationProbe(): { Probe: () => null; current: () => Evaluation } {
  const seen: { ev: Evaluation | null } = { ev: null };
  function Probe() {
    const { evaluation } = useStock();
    useEffect(() => {
      seen.ev = evaluation;
    }, [evaluation]);
    return null;
  }
  return {
    Probe,
    current: () => {
      if (seen.ev === null) {
        throw new Error("no evaluation yet");
      }
      return seen.ev;
    },
  };
}

/** leversWith returns the test levers after the actions. */
export function leversWith(...actions: LeverAction[]): LeverState {
  return actions.reduce(leversReducer, testLevers());
}

/** WhenReady renders its children once the evaluation exists, as the page renders its panels. */
export function WhenReady({ children }: { children: ReactNode }) {
  return useStock().evaluation === null ? null : children;
}
