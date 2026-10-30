"use client";

import type { ReactNode } from "react";
import { useTipHandlers } from "./TipProvider";

/**
 * InfoTip is the small "i" button that explains a term in the tooltip.
 * label names it for screen readers, such as "About TTM EPS".
 */
export function InfoTip({ label, children }: { label: string; children: ReactNode }) {
  const handlers = useTipHandlers(() => children);
  return (
    <button type="button" className="info" aria-label={label} {...handlers}>
      i
    </button>
  );
}
