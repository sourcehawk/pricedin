"use client";

import type { ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useLoadCompany } from "@/api/queries";
import { Panels } from "@/components/panels/Panels";
import { useStock } from "@/state/StockProvider";

function Notice({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <section className="panel empty-state" aria-labelledby="notice-title">
      <h2 id="notice-title">{title}</h2>
      {children}
    </section>
  );
}

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : "Something went wrong.";
}

function LoadCompany({ ticker }: { ticker: string }) {
  const load = useLoadCompany();
  return (
    <>
      <p>
        PricedIn knows {ticker} but has not loaded its filings and prices yet. Loading takes up to a
        minute.
      </p>
      <button
        type="button"
        className="btn"
        disabled={load.isPending}
        onClick={() => {
          load.mutate(ticker);
        }}
      >
        {load.isPending ? `Loading ${ticker}…` : "Load this company"}
      </button>
      {load.isError && (
        <p className="error" role="alert">
          {ticker} could not be loaded. {errorText(load.error)}
        </p>
      )}
    </>
  );
}

/**
 * StockMain is the content of the main column: the panels once the
 * evaluation exists, and otherwise what the owner can do next.
 */
export function StockMain() {
  const { state, restored, profile, history, evaluation, evaluationError } = useStock();
  if (!restored) {
    return null;
  }
  const t = state.ticker;
  if (t === null) {
    return (
      <Notice title="Pick a stock">
        <p>
          Search for a ticker or a company name in the left panel. PricedIn then shows the EPS
          growth that the price already assumes, and whether the company has delivered it.
        </p>
      </Notice>
    );
  }
  // A failed refresh keeps the data read before; the panels stay, with a note.
  if (profile.isError && profile.data === undefined) {
    const unknown = profile.error instanceof ApiError && profile.error.status === 404;
    return (
      <Notice title={unknown ? `No company has the ticker ${t}` : `${t} could not be read`}>
        <p>
          {unknown
            ? "Search again in the left panel. PricedIn covers the stocks that file with the SEC."
            : errorText(profile.error)}
        </p>
      </Notice>
    );
  }
  if (profile.data === undefined) {
    return <Notice title={`Reading ${t}…`} />;
  }
  if (!profile.data.loaded) {
    return (
      <Notice title={`${profile.data.company.name} is not loaded yet`}>
        <LoadCompany ticker={t} />
      </Notice>
    );
  }
  if (history.isError && history.data === undefined) {
    return (
      <Notice title={`The history of ${t} could not be read`}>
        <p>{errorText(history.error)}</p>
      </Notice>
    );
  }
  if (evaluationError !== null) {
    return (
      <Notice title="These inputs cannot be evaluated">
        <p>{evaluationError.message}</p>
      </Notice>
    );
  }
  if (evaluation === null) {
    if (history.data !== undefined) {
      return (
        <Notice title={`${t} has no price or TTM EPS yet`}>
          <p>Type a share price and a TTM EPS in the left panel to evaluate it.</p>
        </Notice>
      );
    }
    return <Notice title={`Reading the history of ${t}…`} />;
  }
  const refresh = profile.isError ? profile.error : history.isError ? history.error : null;
  return (
    <>
      {refresh !== null && (
        <p className="note" role="status">
          {t} could not be refreshed, so the page shows the data read before. {errorText(refresh)}
        </p>
      )}
      <Panels />
    </>
  );
}
