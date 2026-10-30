/**
 * HoverStore holds the index of the hovered day, null when none is. The
 * plot writes it on each pointer move and only its readers re-render, so a
 * hover never re-renders the chart.
 */
export interface HoverStore {
  get: () => number | null;
  set: (i: number | null) => void;
  subscribe: (listener: () => void) => () => void;
}

/** createHoverStore returns an empty store; set notifies only on a change. */
export function createHoverStore(): HoverStore {
  let value: number | null = null;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set: (i) => {
      if (i === value) {
        return;
      }
      value = i;
      for (const l of listeners) {
        l();
      }
    },
    subscribe: (l) => {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
  };
}
