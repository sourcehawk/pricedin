"use client";

import { useEffect, useId, useRef } from "react";
import { useTipHandlers } from "@/components/tip/TipProvider";
import { useStock } from "@/state/StockProvider";
import { BaselineLever, ExitPeLever, HoldingPeriodLever, TargetReturnLever } from "./Levers";
import { RailWalkthrough } from "./RailWalkthrough";
import { StartingPoint } from "./StartingPoint";
import { StockSearch } from "./StockSearch";

/** Below this width the page has one column and the left panel scrolls with the page. */
const singleColumn = 920;

/**
 * useFitToScreen keeps the bottom of a sticky panel at the bottom of the
 * window, wherever its top is, so the panel scrolls on its own.
 */
function useFitToScreen(ref: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    const fit = () => {
      const el = ref.current;
      const parent = el?.parentElement;
      if (!el || !parent) {
        return;
      }
      if (window.innerWidth <= singleColumn) {
        el.style.maxHeight = "";
        return;
      }
      const top = Math.max(16, parent.getBoundingClientRect().top);
      el.style.maxHeight = `${String(Math.max(240, window.innerHeight - top - 16))}px`;
    };
    fit();
    window.addEventListener("scroll", fit, { passive: true });
    window.addEventListener("resize", fit);
    return () => {
      window.removeEventListener("scroll", fit);
      window.removeEventListener("resize", fit);
    };
  }, [ref]);
}

/** Rail is the left panel: the stock search, the starting point, every lever and the walkthrough of the inputs. */
export function Rail() {
  const { dispatch } = useStock();
  const pickId = useId();
  const ref = useRef<HTMLElement>(null);
  useFitToScreen(ref);
  const resetTip = useTipHandlers(
    () =>
      "Puts every input back to its default for this stock, including your target return and holding period.",
  );
  return (
    <aside ref={ref} className="rail" aria-label="Assumptions" data-walk-scroll>
      <div className="field">
        <div className="railhead">
          <label htmlFor={pickId} className="eyebrow">
            Stock
          </label>
          <button
            type="button"
            className="resetbtn"
            {...resetTip}
            onClick={() => {
              dispatch({ type: "reset" });
            }}
          >
            <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
              <path
                d="M3 8a5 5 0 1 0 1.6-3.7"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
              />
              <path
                d="M3 2.5v3h3"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            Reset inputs
          </button>
        </div>
        <StockSearch inputId={pickId} />
      </div>
      <StartingPoint />
      <div className="divider" />
      <TargetReturnLever />
      <HoldingPeriodLever />
      <ExitPeLever />
      <div className="divider" />
      <BaselineLever />
      <RailWalkthrough />
    </aside>
  );
}
