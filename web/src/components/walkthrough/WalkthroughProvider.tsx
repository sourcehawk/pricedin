"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

/** ActiveWalkthrough is the walkthrough that runs and its step, from 0. */
export interface ActiveWalkthrough {
  id: string;
  step: number;
}

/** WalkthroughControl runs one walkthrough at a time. */
export interface WalkthroughControl {
  active: ActiveWalkthrough | null;
  /** Starts id at its first step; ends the one that runs. */
  start: (id: string) => void;
  /** Goes to a step of the walkthrough that runs. */
  go: (step: number) => void;
  end: () => void;
  /** The element of each WalkthroughSlot by name. */
  slots: Readonly<Record<string, HTMLElement>>;
  registerSlot: (name: string, el: HTMLElement | null) => void;
}

const Context = createContext<WalkthroughControl | null>(null);

/** WalkthroughProvider holds the walkthrough that runs; one per page. */
export function WalkthroughProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<ActiveWalkthrough | null>(null);
  const [slots, setSlots] = useState<Record<string, HTMLElement>>({});
  const start = useCallback((id: string) => {
    setActive({ id, step: 0 });
  }, []);
  const go = useCallback((step: number) => {
    setActive((a) => (a === null ? null : { ...a, step }));
  }, []);
  const end = useCallback(() => {
    setActive(null);
  }, []);
  const registerSlot = useCallback((name: string, el: HTMLElement | null) => {
    setSlots((s) => {
      if (el === null) {
        return Object.fromEntries(Object.entries(s).filter(([k]) => k !== name));
      }
      return s[name] === el ? s : { ...s, [name]: el };
    });
  }, []);
  const value = useMemo(
    () => ({ active, start, go, end, slots, registerSlot }),
    [active, start, go, end, slots, registerSlot],
  );
  return <Context value={value}>{children}</Context>;
}

/** useWalkthrough returns the control of the nearest WalkthroughProvider. */
export function useWalkthrough(): WalkthroughControl {
  const c = useContext(Context);
  if (c === null) {
    throw new Error("useWalkthrough needs a WalkthroughProvider");
  }
  return c;
}

/**
 * WalkthroughSlot is a place outside a panel where a walkthrough with the
 * same slot name writes its step card, such as the top of the main column
 * for the left panel's walkthrough.
 */
export function WalkthroughSlot({ name, className }: { name: string; className?: string }) {
  const { registerSlot } = useWalkthrough();
  const ref = useCallback(
    (el: HTMLDivElement | null) => {
      registerSlot(name, el);
    },
    [name, registerSlot],
  );
  return <div ref={ref} className={className} data-walk-slot={name} />;
}
