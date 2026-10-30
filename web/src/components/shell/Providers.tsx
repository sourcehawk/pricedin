"use client";

import { useSyncExternalStore, type ReactNode } from "react";
import { ApiProvider } from "@/api/queries";
import { TipProvider } from "@/components/tip/TipProvider";
import { WalkthroughProvider } from "@/components/walkthrough/WalkthroughProvider";
import { StockProvider } from "@/state/StockProvider";

/** Providers holds what every page needs: the API client, the query cache and the tooltip. */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <ApiProvider>
      <TipProvider>{children}</TipProvider>
    </ApiProvider>
  );
}

const noSubscription = () => () => undefined;

/**
 * AppProviders holds the state of the pages behind the login: the stock,
 * the levers and the walkthrough. The levers live in this browser's
 * storage, so the server renders nothing of them.
 */
export function AppProviders({ children }: { children: ReactNode }) {
  const client = useSyncExternalStore(
    noSubscription,
    () => true,
    () => false,
  );
  if (!client) {
    return null;
  }
  return (
    <StockProvider>
      <WalkthroughProvider>{children}</WalkthroughProvider>
    </StockProvider>
  );
}
