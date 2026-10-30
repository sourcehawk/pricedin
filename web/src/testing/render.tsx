import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, type RenderResult } from "@testing-library/react";
import { useEffect, type ReactElement, type ReactNode } from "react";
import { createApi, type Api } from "@/api/client";
import { ApiProvider } from "@/api/queries";
import { WalkthroughProvider } from "@/components/walkthrough/WalkthroughProvider";
import { TipProvider } from "@/components/tip/TipProvider";
import type { Evaluation } from "@/engine";
import { defaultLevers, leversReducer, type LeverAction, type LeverState } from "@/state/levers";
import { StockProvider, useStock } from "@/state/StockProvider";

/** testLevers are the default levers with ticker picked, as after a pick. */
export function testLevers(ticker = "GRMN"): LeverState {
  return leversReducer(defaultLevers, { type: "pickStock", ticker });
}

/** leversWith returns the test levers after the actions. */
export function leversWith(...actions: LeverAction[]): LeverState {
  return actions.reduce(leversReducer, testLevers());
}

export interface RenderOptions {
  /** The lever state to start from; defaults to GRMN picked. */
  levers?: LeverState;
  api?: Api;
}

/**
 * renderWithStock renders ui inside every provider of the page, against the
 * API that the test's fake server answers (see api/testing/server). It
 * starts from levers instead of this browser's saved state.
 */
export function renderWithStock(ui: ReactElement, opts: RenderOptions = {}): RenderResult {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const api = opts.api ?? createApi();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <ApiProvider api={api} queryClient={queryClient}>
      <StockProvider initial={opts.levers ?? testLevers()}>
        <TipProvider>
          <WalkthroughProvider>{children}</WalkthroughProvider>
        </TipProvider>
      </StockProvider>
    </ApiProvider>
  );
  return render(ui, { wrapper });
}

function ReadyProbe() {
  const { evaluation } = useStock();
  return evaluation === null ? null : <span data-testid="evaluation-ready" />;
}

/** renderReady is renderWithStock that waits until the evaluation exists. */
export async function renderReady(
  ui: ReactElement,
  opts: RenderOptions = {},
): Promise<RenderResult> {
  const r = renderWithStock(
    <>
      <ReadyProbe />
      {ui}
    </>,
    opts,
  );
  await waitFor(() => {
    expect(screen.getByTestId("evaluation-ready")).toBeInTheDocument();
  });
  return r;
}

/** WhenReady renders its children once the evaluation exists, as the page renders its panels. */
export function WhenReady({ children }: { children: ReactNode }) {
  return useStock().evaluation === null ? null : children;
}

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
