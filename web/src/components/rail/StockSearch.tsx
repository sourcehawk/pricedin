"use client";

import { useEffect, useId, useRef, useState } from "react";
import { ApiError } from "@/api/client";
import { useLoadCompany, useSearch } from "@/api/queries";
import type { TickerMatch } from "@/api/types";
import { useStock } from "@/state/StockProvider";

/** debounced returns value once it has not changed for ms. */
function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => {
      setV(value);
    }, ms);
    return () => {
      clearTimeout(t);
    };
  }, [value, ms]);
  return v;
}

function message(e: unknown): string {
  return e instanceof ApiError ? e.message : "Something went wrong.";
}

/**
 * StockSearch is the stock picker: a combobox that searches tickers and
 * company names. A pick of a loaded company selects it; a pick of a
 * company that is not loaded yet loads it first, then selects it.
 */
export function StockSearch({ inputId }: { inputId: string }) {
  const { state, dispatch } = useStock();
  const listId = useId();
  const optionId = useId();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const list = useRef<HTMLUListElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const query = useDebounced(text, 150);
  const search = useSearch(open ? query : "");
  const load = useLoadCompany();
  // The newest pick; a load that finishes after a newer pick selects nothing.
  const lastPick = useRef<string | null>(null);
  // Results show only for the text in the box, so Enter during the debounce
  // cannot pick a match of the text before the last keystroke.
  const hits: TickerMatch[] =
    open && query.trim() !== "" && query === text ? (search.data ?? []) : [];
  const current = Math.min(active, Math.max(0, hits.length - 1));

  // Keep the active option in view inside the list; the page never scrolls.
  useEffect(() => {
    const ul = list.current;
    const li = ul?.children[current] as HTMLElement | undefined;
    if (ul && li) {
      if (li.offsetTop < ul.scrollTop) {
        ul.scrollTop = li.offsetTop;
      } else if (li.offsetTop + li.offsetHeight > ul.scrollTop + ul.clientHeight) {
        ul.scrollTop = li.offsetTop + li.offsetHeight - ul.clientHeight;
      }
    }
  }, [current]);

  const close = () => {
    setOpen(false);
    setText("");
  };

  const pick = (m: TickerMatch) => {
    close();
    input.current?.blur();
    lastPick.current = m.symbol;
    if (m.loaded) {
      dispatch({ type: "pickStock", ticker: m.symbol });
      return;
    }
    load.mutate(m.symbol, {
      onSuccess: () => {
        if (lastPick.current === m.symbol) {
          dispatch({ type: "pickStock", ticker: m.symbol });
        }
      },
    });
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!open && (e.key === "ArrowDown" || e.key === "Enter")) {
      setOpen(true);
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (hits.length > 0) {
        setActive((current + 1) % hits.length);
      }
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (hits.length > 0) {
        setActive((current - 1 + hits.length) % hits.length);
      }
    } else if (e.key === "Enter") {
      e.preventDefault();
      const m = hits[current];
      if (m !== undefined) {
        pick(m);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
      input.current?.blur();
    }
  };

  let empty: string | null = null;
  if (open && hits.length === 0) {
    if (query.trim() === "" || text.trim() === "") {
      empty = "Type a ticker or a company name.";
    } else if (search.isError) {
      empty = `The search failed: ${message(search.error)}`;
    } else if (search.isPending || query !== text) {
      empty = "Searching…";
    } else {
      empty = "No match. Search by ticker, such as AAPL, or by company name.";
    }
  }

  return (
    <div className="pick-box">
      <input
        ref={input}
        id={inputId}
        className="pick"
        type="text"
        role="combobox"
        aria-label="Pick a stock"
        aria-autocomplete="list"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={
          open && hits.length > 0 ? `${optionId}-${String(current)}` : undefined
        }
        autoComplete="off"
        spellCheck={false}
        placeholder="Search ticker or company"
        value={open ? text : (state.ticker ?? "")}
        onFocus={(e) => {
          setOpen(true);
          setText("");
          e.currentTarget.select();
        }}
        onChange={(e) => {
          setText(e.target.value);
          setActive(0);
          setOpen(true);
        }}
        onKeyDown={onKeyDown}
        onBlur={() => {
          close();
        }}
      />
      <ul ref={list} id={listId} className="pick-list" role="listbox" hidden={!open}>
        {hits.map((m, i) => (
          <li
            key={m.symbol}
            id={`${optionId}-${String(i)}`}
            role="option"
            aria-selected={i === current}
            onMouseDown={(e) => {
              e.preventDefault();
              pick(m);
            }}
          >
            <span className="s">{m.symbol}</span>
            <span className="n">{m.name}</span>
            {m.loaded ? (
              <span className="p">{m.exchange}</span>
            ) : (
              <span className="p load">Load this company</span>
            )}
          </li>
        ))}
        {empty !== null && (
          <li className="empty" role="presentation">
            {empty}
          </li>
        )}
      </ul>
      {load.isPending && (
        <p className="pick-status" role="status">
          Loading {load.variables}: its filings and prices. This can take a minute.
        </p>
      )}
      {load.isError && (
        <p className="pick-status error" role="alert">
          {load.variables} could not be loaded. {message(load.error)}
        </p>
      )}
    </div>
  );
}
