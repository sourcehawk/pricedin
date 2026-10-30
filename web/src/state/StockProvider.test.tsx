import { act, render, screen, waitFor } from "@testing-library/react";
import { focusManager, QueryClient } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { memo, useEffect } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApi } from "@/api/client";
import { ApiProvider } from "@/api/queries";
import { exampleHistory, profileFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "@/api/testing/server";
import { evaluate } from "@/engine";
import { renderWithStock, testLevers } from "@/testing/render";
import { engineLevers } from "./levers";
import { saveLevers } from "./storage";
import {
  StockProvider,
  useEvaluation,
  useLeverDispatch,
  useStock,
  type StockContextValue,
} from "./StockProvider";

withApiServer();

afterEach(() => {
  localStorage.clear();
});

let latest: StockContextValue | null = null;
function keep(v: StockContextValue) {
  latest = v;
}
function Probe() {
  const v = useStock();
  useEffect(() => {
    keep(v);
  });
  return null;
}
function current(): StockContextValue {
  if (latest === null) {
    throw new Error("not rendered");
  }
  return latest;
}

describe("StockProvider", () => {
  it("evaluates the stock with the engine, from the stock's price and TTM EPS", async () => {
    renderWithStock(<Probe />);
    await waitFor(() => {
      expect(current().evaluation).not.toBeNull();
    });
    const want = evaluate(
      exampleHistory,
      engineLevers(testLevers(), { price: 270, ttmEps: 10.8, dividend: 0 }, null) ?? fail(),
    );
    expect(current().evaluation?.required).toEqual(want.required);
    expect(current().levers).toMatchObject({ price: 270, ttmEps: 10.8, dividend: 0 });
  });

  it("pins the default cases of the first evaluation", async () => {
    renderWithStock(<Probe />);
    await waitFor(() => {
      expect(current().state.scenarios).not.toBeNull();
    });
    const pinned = current().state.scenarios;
    act(() => {
      current().dispatch({ type: "setTargetReturn", value: 0.1 });
    });
    expect(current().state.scenarios).toEqual(pinned);
    expect(pinned?.map((c) => c.name)).toEqual(["Bear", "Base", "Bull"]);
  });

  it("recomputes on a lever change and marks a typed value as the user's", async () => {
    renderWithStock(<Probe />);
    await waitFor(() => {
      expect(current().evaluation).not.toBeNull();
    });
    expect(current().provenance.price).toMatchObject({ kind: "stock" });
    const before = current().evaluation?.required.growth.value;
    act(() => {
      current().dispatch({ type: "setStartingPoint", field: "price", value: 200 });
    });
    expect(current().evaluation?.required.growth.value).toBeLessThan(before ?? 0);
    expect(current().provenance.price).toEqual({ kind: "user" });
  });

  it("uses no dividend when the stock has no dividend data", async () => {
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:t"), () =>
        HttpResponse.json({
          ...profileFixture,
          ttmDividend: { value: null, reason: "no_dividend_data" },
        }),
      ),
    );
    renderWithStock(<Probe />);
    await waitFor(() => {
      expect(current().evaluation).not.toBeNull();
    });
    expect(current().levers?.dividend).toBe(0);
    expect(current().provenance.dividend).toBeNull();
  });

  it("evaluates with the profile of the history it keeps after a newer history fails", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const { unmount } = renderWithStock(<Probe />);
    try {
      await waitFor(() => {
        expect(current().evaluation).not.toBeNull();
      });
      const price = current().levers?.price;
      // The next profile has a newer close at a new price, and its history fails.
      apiServer.use(
        http.get(apiUrl("/api/v1/stocks/:t"), () =>
          HttpResponse.json({
            ...profileFixture,
            latestClose: profileFixture.latestClose && {
              ...profileFixture.latestClose,
              value: 27,
            },
            freshness: { ...profileFixture.freshness, latestClose: "2099-01-01" },
          }),
        ),
        http.get(apiUrl("/api/v1/stocks/:t/history"), () => problem(503, "Unavailable")),
      );
      vi.setSystemTime(Date.now() + 5 * 60_000 + 1);
      act(() => {
        focusManager.setFocused(false);
        focusManager.setFocused(true);
      });
      await waitFor(() => {
        expect(current().history.isError).toBe(true);
      });
      // The context shows the profile that was read with the kept history.
      expect(current().profile.data?.latestClose?.value).toBe(price);
      expect(current().levers?.price).toBe(price);
      expect(current().evaluation).not.toBeNull();
    } finally {
      // A focus change refetches the queries of a mounted page.
      unmount();
      vi.useRealTimers();
      focusManager.setFocused(undefined);
    }
  });

  it("does not read the history of a stock whose data is not loaded", async () => {
    const history = vi.fn();
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:t/history"), () => {
        history();
      }),
    );
    renderWithStock(<Probe />, { levers: testLevers("KO") });
    await waitFor(() => {
      expect(current().profile.data?.loaded).toBe(false);
    });
    expect(history).not.toHaveBeenCalled();
    expect(current().evaluation).toBeNull();
  });

  it("does not re-render a component that only dispatches when a lever changes", async () => {
    let renders = 0;
    const OnlyDispatch = memo(function OnlyDispatch() {
      useLeverDispatch();
      renders++;
      return null;
    });
    renderWithStock(
      <>
        <Probe />
        <OnlyDispatch />
      </>,
    );
    await waitFor(() => {
      expect(current().evaluation).not.toBeNull();
    });
    const before = renders;
    act(() => {
      current().dispatch({ type: "setTargetReturn", value: 0.1 });
    });
    expect(renders).toBe(before);
  });

  it("gives panels the lever state that their evaluation was computed from", async () => {
    let seen: { target: number; levers: number } | null = null;
    const see = (v: { target: number; levers: number }) => {
      seen = v;
    };
    function Panel() {
      const { state, levers } = useEvaluation();
      useEffect(() => {
        see({ target: state.targetReturn, levers: levers.targetReturn });
      });
      return null;
    }
    function Ready() {
      return useStock().evaluation === null ? null : <Panel />;
    }
    renderWithStock(
      <>
        <Probe />
        <Ready />
      </>,
    );
    await waitFor(() => {
      expect(seen).not.toBeNull();
    });
    act(() => {
      current().dispatch({ type: "setTargetReturn", value: 0.1 });
    });
    await waitFor(() => {
      expect(seen).toEqual({ target: 0.1, levers: 0.1 });
    });
  });

  it("opens the stock saved in this browser", async () => {
    saveLevers(testLevers("GRMN"));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <ApiProvider api={createApi()} queryClient={client}>
        <StockProvider>
          <Probe />
          <span>shell</span>
        </StockProvider>
      </ApiProvider>,
    );
    expect(screen.getByText("shell")).toBeInTheDocument();
    await waitFor(() => {
      expect(current().evaluation).not.toBeNull();
    });
    expect(current().state.ticker).toBe("GRMN");
    expect(current().restored).toBe(true);
  });
});

function fail(): never {
  throw new Error("no levers");
}
