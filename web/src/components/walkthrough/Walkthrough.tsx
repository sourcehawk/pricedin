"use client";

import { useEffect, useLayoutEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { Evaluation } from "@/engine";
import { useStock } from "@/state/StockProvider";
import type { LeverKey } from "@/state/levers";
import { clearLevers, clearScope, markScope, ringLevers } from "./marks";
import { useWalkthrough } from "./WalkthroughProvider";

/** WalkthroughStep is one step of a walkthrough. */
export interface WalkthroughStep {
  title: ReactNode;
  body: ReactNode;
  /** One line with the user's own numbers; shown when an evaluation exists. */
  you?: (ev: Evaluation) => ReactNode;
  /** The data-walk keys of the panel's elements that this step explains. */
  highlight?: readonly string[];
  /** The levers of the left panel that this step refers to; they get the ring. */
  levers?: readonly LeverKey[];
  /** The one-line take-away; give it on the last step. */
  takeaway?: ReactNode | ((ev: Evaluation) => ReactNode);
}

export interface WalkthroughProps {
  /** Unique on the page. */
  id: string;
  /** What it walks through, after "Walkthrough of": "the inputs". */
  name: string;
  /** The launcher's text. */
  launcher: ReactNode;
  steps: readonly WalkthroughStep[];
  /** Shown on the first step, under the title. */
  intro?: ReactNode;
  /** Shown on the last step, under the body: the "Watch out" note. */
  watchOut?: ReactNode;
  /** The WalkthroughSlot that shows the step card; without it, the card takes the launcher's place. */
  slot?: string;
  /** Runs when the walkthrough starts; the function it returns runs when it ends. */
  onStart?: () => (() => void) | undefined;
}

function safely(f: () => ReactNode): ReactNode {
  try {
    return f();
  } catch {
    // A line that cannot be computed for these numbers is left out.
    return null;
  }
}

function isTyping(t: EventTarget | null): boolean {
  return t instanceof HTMLElement && /^(INPUT|SELECT|TEXTAREA)$/.test(t.tagName);
}

/**
 * Walkthrough is a panel's inline walkthrough: a launcher button that
 * starts it, and a step card written into the panel. While a step shows,
 * the panel (the nearest ancestor with data-walk-scope) fades except the
 * elements whose data-walk lists one of the step's highlight keys, and the
 * levers of the step get a ring in the left panel. It never opens a modal
 * and never scrolls the page. Arrow keys move between steps and Escape
 * ends it. One walkthrough runs at a time.
 */
export function Walkthrough(props: WalkthroughProps) {
  const { id, name, launcher, steps, intro, watchOut, slot, onStart } = props;
  const control = useWalkthrough();
  const { evaluation } = useStock();
  const running = control.active?.id === id;
  const index = running ? Math.min(control.active?.step ?? 0, steps.length - 1) : 0;
  const step = steps[index];

  const wrap = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const next = useRef<HTMLButtonElement>(null);
  const open = useRef<HTMLButtonElement>(null);
  const wasRunning = useRef(false);
  const onStartRef = useRef(onStart);
  useEffect(() => {
    onStartRef.current = onStart;
  });

  // Setup and teardown of a run.
  useEffect(() => {
    if (!running) {
      return;
    }
    const cleanup = onStartRef.current?.();
    return () => {
      if (typeof cleanup === "function") {
        cleanup();
      }
    };
  }, [running]);

  // An attribute that no component renders survives a re-render. The marks
  // set only attributes, so they do not wake the observer.
  useLayoutEffect(() => {
    const scope = wrap.current?.closest("[data-walk-scope]") ?? null;
    if (!running || step === undefined) {
      return;
    }
    const highlight = step.highlight ?? [];
    const levers = step.levers ?? [];
    const apply = () => {
      if (scope !== null) {
        markScope(scope, highlight, card.current);
      }
      ringLevers(document, levers, false);
    };
    apply();
    const observer = new MutationObserver(apply);
    observer.observe(document.body, { childList: true, subtree: true });
    return () => {
      observer.disconnect();
      if (scope !== null) {
        clearScope(scope);
      }
      clearLevers(document);
    };
  });

  // The left panel scrolls to the first ringed lever once per step.
  const leverKeys = step?.levers?.join(" ") ?? "";
  useLayoutEffect(() => {
    if (running && leverKeys !== "") {
      ringLevers(document, leverKeys.split(" "));
    }
  }, [running, index, leverKeys]);

  // Focus moves to Next on each step, and back to the launcher at the end,
  // without scrolling. When another walkthrough took over, its card keeps
  // the focus.
  const anyRunning = control.active !== null;
  useEffect(() => {
    if (running) {
      next.current?.focus({ preventScroll: true });
    } else if (wasRunning.current && !anyRunning) {
      open.current?.focus({ preventScroll: true });
    }
    wasRunning.current = running;
  }, [running, index, anyRunning]);

  const { go, end } = control;
  useEffect(() => {
    if (!running) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      // A control that used the key, such as an open list, prevents it.
      if (e.defaultPrevented) {
        return;
      }
      if (e.key === "Escape") {
        end();
      } else if (e.key === "ArrowRight" && !isTyping(e.target)) {
        e.preventDefault();
        if (index < steps.length - 1) {
          go(index + 1);
        }
      } else if (e.key === "ArrowLeft" && !isTyping(e.target)) {
        e.preventDefault();
        if (index > 0) {
          go(index - 1);
        }
      }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
    };
  }, [running, index, steps.length, go, end]);

  const launcherButton = (
    <button
      ref={open}
      type="button"
      className="guide-open"
      aria-expanded={running}
      onClick={() => {
        control.start(id);
      }}
    >
      <span className="q" aria-hidden="true">
        ?
      </span>
      {launcher}
    </button>
  );

  if (!running || step === undefined) {
    return (
      <div ref={wrap} className="guide">
        {launcherButton}
      </div>
    );
  }

  const last = index === steps.length - 1;
  const you = evaluation !== null && step.you ? safely(() => step.you?.(evaluation)) : null;
  const takeaway =
    typeof step.takeaway === "function"
      ? evaluation === null
        ? null
        : safely(() => (step.takeaway as (ev: Evaluation) => ReactNode)(evaluation))
      : (step.takeaway ?? null);

  const stepCard = (
    <div
      ref={card}
      className="tour-card"
      role="region"
      aria-label={`Walkthrough of ${name}`}
      aria-live="polite"
    >
      <div className="t-head">
        <span className="t-step">
          Walkthrough · step {index + 1} of {steps.length}
        </span>
        <button type="button" className="t-x" aria-label="End the walkthrough" onClick={end}>
          ×
        </button>
      </div>
      <div className="t-body">
        <div className="t-text">
          <h4>{step.title}</h4>
          {index === 0 && intro !== undefined && <div className="t-extra">{intro}</div>}
          <div>{step.body}</div>
          {last && watchOut !== undefined && (
            <div className="t-extra">
              <b>Watch out.</b> {watchOut}
            </div>
          )}
        </div>
        <div className="t-side">
          {you !== null && you !== undefined && you !== false && <div className="t-you">{you}</div>}
          {takeaway !== null && takeaway !== undefined && (
            <div className="t-you t-takeaway">{takeaway}</div>
          )}
        </div>
      </div>
      <div className="t-nav">
        <button
          type="button"
          className="prev"
          disabled={index === 0}
          onClick={() => {
            go(index - 1);
          }}
        >
          Back
        </button>
        <span className="dots" aria-hidden="true">
          {steps.map((_, j) => (
            <i key={j} className={j === index ? "on" : ""} />
          ))}
        </span>
        <button
          ref={next}
          type="button"
          className="next"
          onClick={() => {
            if (last) {
              end();
            } else {
              go(index + 1);
            }
          }}
        >
          {last ? "Finish" : "Next"}
        </button>
      </div>
    </div>
  );

  const target = slot === undefined ? undefined : control.slots[slot];
  if (target !== undefined) {
    return (
      <div ref={wrap} className="guide">
        {launcherButton}
        {createPortal(stepCard, target)}
      </div>
    );
  }
  return (
    <div ref={wrap} className="guide guide-running">
      {stepCard}
    </div>
  );
}
