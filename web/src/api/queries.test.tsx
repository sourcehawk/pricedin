import { QueryClient } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { createApi } from "./client";
import { ApiProvider, useHistory } from "./queries";
import { historyFixture } from "./testing/fixtures";
import { apiServer, apiUrl, withApiServer } from "./testing/server";
import type { Profile } from "./types";

withApiServer();

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <ApiProvider api={createApi()} queryClient={client}>
      {children}
    </ApiProvider>
  );
}

describe("useHistory", () => {
  it("reads the history again when the profile reports newer data", async () => {
    let reads = 0;
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:t/history"), () => {
        reads++;
        return HttpResponse.json(historyFixture);
      }),
    );
    const before: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-28" };
    const after: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-29" };
    const { result, rerender } = renderHook(
      ({ freshness }: { freshness: Profile["freshness"] }) => useHistory("GRMN", freshness),
      { wrapper, initialProps: { freshness: before } },
    );
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    rerender({ freshness: before });
    expect(reads).toBe(1);
    rerender({ freshness: after });
    await waitFor(() => {
      expect(reads).toBe(2);
    });
  });

  it("keeps the previous history of the same stock while it reads the newer one", async () => {
    let release: () => void = () => undefined;
    let reads = 0;
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:t/history"), () => {
        reads++;
        if (reads === 1) {
          return HttpResponse.json(historyFixture);
        }
        return new Promise<Response>((resolve) => {
          release = () => {
            resolve(HttpResponse.json(historyFixture));
          };
        });
      }),
    );
    const before: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-28" };
    const after: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-29" };
    const { result, rerender } = renderHook(
      ({ ticker, freshness }: { ticker: string; freshness: Profile["freshness"] }) =>
        useHistory(ticker, freshness),
      { wrapper, initialProps: { ticker: "GRMN", freshness: before } },
    );
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    rerender({ ticker: "GRMN", freshness: after });
    await waitFor(() => {
      expect(reads).toBe(2);
    });
    expect(result.current.data).toEqual(historyFixture);
    expect(result.current.isPlaceholderData).toBe(true);
    release();
    rerender({ ticker: "KO", freshness: after });
    expect(result.current.data).toBeUndefined();
  });

  it("is idle without the freshness of a loaded stock", () => {
    const { result } = renderHook(() => useHistory("GRMN", null), { wrapper });
    expect(result.current.fetchStatus).toBe("idle");
  });
});
