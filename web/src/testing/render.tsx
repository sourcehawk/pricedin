import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, type RenderResult } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { createApi, type Api } from "@/api/client";
import { ApiProvider } from "@/api/queries";
import { WalkthroughProvider } from "@/components/walkthrough/WalkthroughProvider";
import { TipProvider } from "@/components/tip/TipProvider";
import { defaultLevers, leversReducer, type LeverState } from "@/state/levers";
import { StockProvider, useStock } from "@/state/StockProvider";

/** testLevers are the default levers with ticker picked, as after a pick. */
export function testLevers(ticker = "GRMN"): LeverState {
  return leversReducer(defaultLevers, { type: "pickStock", ticker });
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
