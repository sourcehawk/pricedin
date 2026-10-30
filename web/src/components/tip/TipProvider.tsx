"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

/** tipId is the id of the one tooltip of the page, for aria-describedby. */
export const tipId = "pricedin-tip";

interface TipState {
  owner: Element;
  content: ReactNode;
  /** The pointer position; null places the tip under the owner. */
  at: { x: number; y: number } | null;
}

/** Tip shows the page's one tooltip, the prototype's dark tooltip that follows the pointer. */
export interface Tip {
  /** Shows content for owner, at the pointer position or, without one, under owner. */
  show: (owner: Element, content: ReactNode, at?: { x: number; y: number }) => void;
  /** Moves the tooltip of owner to the pointer. */
  move: (owner: Element, at: { x: number; y: number }) => void;
  /** Replaces the content of the tooltip of owner, where it is; does nothing when owner shows none. */
  update: (owner: Element, content: ReactNode) => void;
  /** Hides the tooltip when owner shows it, or any tooltip without owner. */
  hide: (owner?: Element) => void;
  /** Hides the tooltip of owner soon, unless the pointer moves onto the tooltip first. */
  leave: (owner: Element) => void;
  /** The element whose tooltip shows; null when none shows. */
  owner: Element | null;
}

const TipContext = createContext<Tip | null>(null);

/** leaveDelay gives the pointer time to move from an element onto its tooltip. */
const leaveDelay = 150;

/** TipProvider renders the tooltip layer. */
export function TipProvider({ children }: { children: ReactNode }) {
  const [tip, setTip] = useState<TipState | null>(null);
  const el = useRef<HTMLDivElement>(null);
  const leaving = useRef<{ owner: Element; timer: ReturnType<typeof setTimeout> } | null>(null);

  const stay = useCallback(() => {
    if (leaving.current !== null) {
      clearTimeout(leaving.current.timer);
      leaving.current = null;
    }
  }, []);
  const show = useCallback(
    (owner: Element, content: ReactNode, at?: { x: number; y: number }) => {
      stay();
      setTip({ owner, content, at: at ?? null });
    },
    [stay],
  );
  const move = useCallback((owner: Element, at: { x: number; y: number }) => {
    setTip((t) => (t !== null && t.owner === owner ? { ...t, at } : t));
  }, []);
  const update = useCallback((owner: Element, content: ReactNode) => {
    setTip((t) => (t !== null && t.owner === owner ? { ...t, content } : t));
  }, []);
  const hide = useCallback(
    (owner?: Element) => {
      // Another owner's hide must not keep a leaving tooltip open.
      if (owner === undefined || leaving.current?.owner === owner) {
        stay();
      }
      setTip((t) => (t === null || owner === undefined || t.owner === owner ? null : t));
    },
    [stay],
  );
  const leave = useCallback(
    (owner: Element) => {
      stay();
      const timer = setTimeout(() => {
        leaving.current = null;
        setTip((t) => (t !== null && t.owner === owner ? null : t));
      }, leaveDelay);
      leaving.current = { owner, timer };
    },
    [stay],
  );
  useEffect(() => stay, [stay]);

  // A tooltip that a tap or focus opened sits under its element and would
  // stay behind when the element scrolls away. Capture sees every scroll.
  const placedUnder = tip !== null && tip.at === null;
  useEffect(() => {
    if (!placedUnder) {
      return;
    }
    const onScroll = () => {
      hide();
    };
    document.addEventListener("scroll", onScroll, { capture: true, passive: true });
    return () => {
      document.removeEventListener("scroll", onScroll, { capture: true });
    };
  }, [placedUnder, hide]);

  useEffect(() => {
    if (tip === null) {
      return;
    }
    // Capture runs before the walkthrough's listener, and preventDefault
    // tells it that this Escape is used up.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setTip(null);
      }
    };
    document.addEventListener("keydown", onKey, { capture: true });
    return () => {
      document.removeEventListener("keydown", onKey, { capture: true });
    };
  }, [tip]);

  // Place the tooltip next to the pointer, inside the window.
  useLayoutEffect(() => {
    const node = el.current;
    if (tip === null || node === null) {
      return;
    }
    let { x, y } = tip.at ?? { x: 0, y: 0 };
    if (tip.at === null) {
      const b = tip.owner.getBoundingClientRect();
      x = b.left + b.width / 2;
      y = b.bottom;
    }
    const w = node.offsetWidth;
    const h = node.offsetHeight;
    let left = x + 14;
    let top = y + 14;
    if (left + w > window.innerWidth - 8) {
      left = x - w - 14;
    }
    if (left < 8) {
      left = 8;
    }
    if (top + h > window.innerHeight - 8) {
      top = y - h - 14;
    }
    if (top < 8) {
      top = 8;
    }
    node.style.left = `${String(left)}px`;
    node.style.top = `${String(top)}px`;
  }, [tip]);

  const value = useMemo<Tip>(
    () => ({ show, move, update, hide, leave, owner: tip?.owner ?? null }),
    [show, move, update, hide, leave, tip?.owner],
  );
  return (
    <TipContext value={value}>
      {children}
      {tip !== null && (
        <div
          ref={el}
          id={tipId}
          role="tooltip"
          className="tip"
          onPointerEnter={stay}
          onPointerLeave={() => {
            hide();
          }}
        >
          {tip.content}
        </div>
      )}
    </TipContext>
  );
}

/** useTip returns the tooltip of the nearest TipProvider. */
export function useTip(): Tip {
  const t = useContext(TipContext);
  if (t === null) {
    throw new Error("useTip needs a TipProvider");
  }
  return t;
}

/**
 * useTipHandlers returns the props that show content as the tooltip of an
 * element: on hover with a mouse or a pen, on keyboard focus, and on a tap,
 * which toggles it. Spread them on the element, ref included. Pass as data
 * the value that content reads, and a new value when it changes: an open
 * tooltip then shows the new content. The tooltip closes when the element
 * unmounts.
 */
export function useTipHandlers(content: () => ReactNode, data?: unknown) {
  const tip = useTip();
  const pointer = useRef<string>("mouse");
  // A tap focuses the element before its click, and focus shows the tip,
  // so the toggle reads whether the tip showed before the tap began.
  const openAtTap = useRef(false);
  const [node, setNode] = useState<Element | null>(null);
  const latest = useRef(content);
  useLayoutEffect(() => {
    latest.current = content;
  });
  const { update, hide } = tip;
  useEffect(() => {
    if (node !== null) {
      update(node, latest.current());
    }
  }, [data, node, update]);
  useEffect(
    () => () => {
      if (node !== null) {
        hide(node);
      }
    },
    [node, hide],
  );
  return {
    ref: setNode,
    "aria-describedby": node !== null && tip.owner === node ? tipId : undefined,
    onPointerDown: (e: React.PointerEvent) => {
      pointer.current = e.pointerType;
      openAtTap.current = tip.owner === e.currentTarget;
    },
    onPointerEnter: (e: React.PointerEvent) => {
      if (e.pointerType !== "touch") {
        tip.show(e.currentTarget, content(), { x: e.clientX, y: e.clientY });
      }
    },
    onPointerMove: (e: React.PointerEvent) => {
      if (e.pointerType !== "touch") {
        tip.move(e.currentTarget, { x: e.clientX, y: e.clientY });
      }
    },
    onPointerLeave: (e: React.PointerEvent) => {
      if (e.pointerType !== "touch") {
        tip.leave(e.currentTarget);
      }
    },
    onFocus: (e: React.FocusEvent) => {
      tip.show(e.currentTarget, content());
    },
    onBlur: (e: React.FocusEvent) => {
      tip.hide(e.currentTarget);
    },
    onClick: (e: React.MouseEvent) => {
      if (pointer.current !== "touch") {
        return;
      }
      if (openAtTap.current) {
        tip.hide(e.currentTarget);
      } else {
        tip.show(e.currentTarget, content());
      }
    },
  };
}
