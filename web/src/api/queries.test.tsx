import { focusManager, QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApi } from "./client";
import { ApiProvider, newQueryClient, useHistory, useProfile } from "./queries";
import { historyFixture, profileFixture } from "./testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "./testing/server";
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

  it("keeps the last history of the stock when a newer one fails to load", async () => {
    let reads = 0;
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/GRMN/history"), () => {
        reads++;
        return reads === 1 ? HttpResponse.json(historyFixture) : problem(503, "Unavailable");
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
    rerender({ freshness: after });
    await waitFor(() => {
      expect(reads).toBe(2);
    });
    await waitFor(() => {
      expect(result.current.isError).toBe(true);
    });
    expect(result.current.data).toEqual(historyFixture);
  });

  it("reads a failed history again when the window gets the focus", async () => {
    let reads = 0;
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/GRMN/history"), () => {
        reads++;
        return reads === 2 ? problem(503, "Unavailable") : HttpResponse.json(historyFixture);
      }),
    );
    const before: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-28" };
    const after: Profile["freshness"] = { latestFiled: "2026-08-01", latestClose: "2026-10-29" };
    const client = newQueryClient();
    client.setDefaultOptions({ queries: { ...client.getDefaultOptions().queries, retry: false } });
    const { result, rerender, unmount } = renderHook(
      ({ freshness }: { freshness: Profile["freshness"] }) => useHistory("GRMN", freshness),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <ApiProvider api={createApi()} queryClient={client}>
            {children}
          </ApiProvider>
        ),
        initialProps: { freshness: before },
      },
    );
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    rerender({ freshness: after });
    await waitFor(() => {
      expect(result.current.isError).toBe(true);
    });
    act(() => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
    });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(reads).toBe(3);
    unmount();
    focusManager.setFocused(undefined);
  });

  it("is idle without the freshness of a loaded stock", () => {
    const { result } = renderHook(() => useHistory("GRMN", null), { wrapper });
    expect(result.current.fetchStatus).toBe("idle");
  });
});

describe("useProfile", () => {
  afterEach(() => {
    vi.useRealTimers();
    focusManager.setFocused(undefined);
  });

  it("reads the profile again when the window gets the focus after it went stale", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    let reads = 0;
    apiServer.use(
      http.get(apiUrl("/api/v1/stocks/:t"), () => {
        reads++;
        return HttpResponse.json(profileFixture);
      }),
    );
    // The app's own query client, which turns the refetch on focus off by default.
    const client = newQueryClient();
    const { result } = renderHook(() => useProfile("GRMN"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <ApiProvider api={createApi()} queryClient={client}>
          {children}
        </ApiProvider>
      ),
    });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    const refocus = () => {
      act(() => {
        focusManager.setFocused(false);
        focusManager.setFocused(true);
      });
    };
    refocus();
    expect(reads).toBe(1);
    vi.setSystemTime(Date.now() + 5 * 60_000 + 1);
    refocus();
    await waitFor(() => {
      expect(reads).toBe(2);
    });
  });
});
