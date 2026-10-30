"use client";

import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { createContext, useContext, useState, type ReactNode } from "react";
import { ApiError, createApi, type Api } from "./client";
import { loginPath } from "./session";
import type { Profile, StockHistory } from "./types";

const ApiContext = createContext<Api | null>(null);

/** newQueryClient returns the query client of the app: no retry of a 4xx answer, and no refetch on focus unless a query asks for it. */
export function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failures, err) =>
          !(err instanceof ApiError && err.status >= 400 && err.status < 500) && failures < 2,
        refetchOnWindowFocus: false,
      },
    },
  });
}

function browserApi(): Api {
  return createApi({
    onSessionExpired: () => {
      window.location.assign(loginPath(window.location, true));
    },
  });
}

/**
 * ApiProvider gives its children the API client and the query cache. Tests
 * pass their own api or queryClient; the default client sends the browser
 * to the login page when the session ends.
 */
export function ApiProvider({
  api,
  queryClient,
  children,
}: {
  api?: Api;
  queryClient?: QueryClient;
  children: ReactNode;
}) {
  const [value] = useState(() => api ?? browserApi());
  const [client] = useState(() => queryClient ?? newQueryClient());
  return (
    <ApiContext value={value}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </ApiContext>
  );
}

/** useApi returns the API client of the nearest ApiProvider. */
export function useApi(): Api {
  const api = useContext(ApiContext);
  if (api === null) {
    throw new Error("useApi needs an ApiProvider");
  }
  return api;
}

/** stockKey is the query key prefix of everything cached for one stock. */
export const stockKey = (ticker: string) => ["stock", ticker] as const;

/**
 * useProfile reads the profile of ticker; it is idle while ticker is null.
 * A window that gets the focus back reads a profile older than its stale
 * time again, so a tab left open overnight shows the new close.
 */
export function useProfile(ticker: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: [...stockKey(ticker ?? ""), "profile"],
    queryFn: () => api.profile(ticker ?? ""),
    enabled: ticker !== null,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: true,
  });
}

/**
 * useHistory reads the history the engine runs on. When ticker is set and
 * freshness changes to a value that is not cached, the history is read
 * again. It is idle while freshness is null, such as before the profile says
 * that the stock is loaded. When a newer history of the same stock fails to
 * load, isError is true and data stays the last history read.
 */
export function useHistory(ticker: string | null, freshness: Profile["freshness"] | null) {
  const api = useApi();
  const q = useQuery({
    queryKey: [
      ...stockKey(ticker ?? ""),
      "history",
      freshness?.latestFiled ?? null,
      freshness?.latestClose ?? null,
    ],
    queryFn: () => api.history(ticker ?? "", "3Y"),
    enabled: ticker !== null && freshness !== null,
    // While a newer history of the same stock loads, the panels keep the
    // previous one instead of unmounting.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === ticker ? previous : undefined,
    staleTime: Number.POSITIVE_INFINITY,
    // Only a history that failed to load is stale, so focus reads that one again.
    refetchOnWindowFocus: true,
    gcTime: 60 * 60_000,
  });
  const [kept, setKept] = useState<StockHistory | undefined>(undefined);
  if (q.data !== undefined && !q.isPlaceholderData && q.data !== kept) {
    setKept(q.data);
  }
  const data = q.data ?? (kept !== undefined && kept.ticker === ticker ? kept : undefined);
  return { ...q, data };
}

/** useSearch searches tickers and names; it is idle for an empty query. */
export function useSearch(q: string) {
  const api = useApi();
  const query = q.trim();
  return useQuery({
    queryKey: ["search", query.toLowerCase()],
    queryFn: () => api.search(query, 10),
    enabled: query !== "",
    staleTime: 60_000,
  });
}

/** useStatus reads the data freshness and the latest ingest runs. */
export function useStatus() {
  const api = useApi();
  return useQuery({ queryKey: ["status"], queryFn: () => api.status() });
}

/** useLoadCompany loads or refreshes one company, then drops every cached value of it. */
export function useLoadCompany() {
  const api = useApi();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (ticker: string) => api.load(ticker),
    onSuccess: async (_profile, ticker) => {
      await Promise.all([
        client.invalidateQueries({ queryKey: stockKey(ticker) }),
        client.invalidateQueries({ queryKey: ["search"] }),
        client.invalidateQueries({ queryKey: ["status"] }),
      ]);
    },
  });
}
