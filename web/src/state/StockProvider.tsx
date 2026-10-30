"use client";

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useDeferredValue,
  useSyncExternalStore,
  type Dispatch,
  type ReactNode,
} from "react";
import { useHistory, useProfile } from "@/api/queries";
import type { Profile, SourcedNumber, StockHistory } from "@/api/types";
import {
  evaluate,
  InvalidInputError,
  trajectoryFill,
  type Evaluation,
  type Levers,
} from "@/engine";
import {
  defaultLevers,
  engineLevers,
  leversReducer,
  type LeverAction,
  type LeverState,
  type StartingPointField,
  type ViewValue,
} from "./levers";
import { loadLevers, saveLevers } from "./storage";

/** Provenance is where a starting-point value comes from: the stock's data, or the user. */
export type Provenance = { kind: "stock"; sourced: SourcedNumber } | { kind: "user" } | null;

/** StockContextValue is everything the page knows about the selected stock and the levers. */
export interface StockContextValue {
  /** The levers as the user set them now. */
  state: LeverState;
  /**
   * The levers that levers and evaluation were computed from. While the
   * user drags a slider, they can trail state by a render.
   */
  evaluatedState: LeverState;
  dispatch: Dispatch<LeverAction>;
  /** True once the saved levers of this browser are read; the page waits for it before it shows a stock. */
  restored: boolean;
  profile: ReturnType<typeof useProfile>;
  history: ReturnType<typeof useHistory>;
  /** The engine's levers; null until the stock gives a price and a TTM EPS. */
  levers: Levers | null;
  /** The engine output for the history and the levers; null while either is missing or invalid. */
  evaluation: Evaluation | null;
  /** Set when the engine rejects the history or the levers. */
  evaluationError: InvalidInputError | null;
  /** The source of each starting-point value in use. */
  provenance: Record<StartingPointField, Provenance>;
}

/** StockData is the stock context without the dispatch, which has a context of its own. */
type StockData = Omit<StockContextValue, "dispatch">;

const StockContext = createContext<StockData | null>(null);
const DispatchContext = createContext<Dispatch<LeverAction> | null>(null);

const noSubscription = () => () => undefined;

/** present returns v when it holds a value: the API reports a dividend that was never fetched as missing. */
function present(v: SourcedNumber | { value: null } | null | undefined): SourcedNumber | null {
  return v === null || v === undefined || v.value === null ? null : v;
}

function stockStartingPoint(p: Profile | undefined) {
  return {
    price: p?.latestClose?.value ?? null,
    ttmEps: p?.ttmEps?.value ?? null,
    dividend: present(p?.ttmDividend)?.value ?? null,
  };
}

function provenanceOf(typed: number | null, sourced: SourcedNumber | null | undefined): Provenance {
  if (typed !== null) {
    return { kind: "user" };
  }
  return sourced ? { kind: "stock", sourced } : null;
}

/**
 * StockProvider holds the lever state, reads the selected stock through the
 * API, and runs the engine on every change. The levers persist in this
 * browser; a saved ticker opens again on the next visit. initial replaces
 * the saved levers, for tests.
 */
export function StockProvider({
  children,
  initial,
}: {
  children: ReactNode;
  initial?: LeverState;
}) {
  // Storage exists only in the browser. The server renders the defaults,
  // and the page shows the levers only once restored is true, so the
  // first client render matches the server's HTML.
  const [state, dispatch] = useReducer(
    leversReducer,
    initial,
    (i) => i ?? (typeof window === "undefined" ? null : loadLevers()) ?? defaultLevers,
  );
  const client = useSyncExternalStore(
    noSubscription,
    () => true,
    () => false,
  );
  const restored = initial !== undefined || client;

  useEffect(() => {
    if (restored) {
      saveLevers(state);
    }
  }, [restored, state]);

  const profile = useProfile(state.ticker);
  const loaded = profile.data?.loaded === true;
  const history = useHistory(state.ticker, loaded ? (profile.data?.freshness ?? null) : null);

  // The engine and the panels run on a deferred copy of the levers, so a
  // slider drag stays responsive while the panels catch up.
  const evaluated = useDeferredValue(state);
  const quarters = history.data?.history.quarters;
  const trajectoryRates = useMemo(() => {
    if (quarters === undefined) {
      return null;
    }
    const t = trajectoryFill(quarters, evaluated.holdingYears);
    return t.value === null ? null : t.value.rates;
  }, [quarters, evaluated.holdingYears]);

  const levers = useMemo(
    () =>
      loaded ? engineLevers(evaluated, stockStartingPoint(profile.data), trajectoryRates) : null,
    [loaded, evaluated, profile.data, trajectoryRates],
  );

  const result = useMemo((): { ev: Evaluation | null; err: InvalidInputError | null } => {
    if (levers === null || history.data === undefined) {
      return { ev: null, err: null };
    }
    try {
      return { ev: evaluate(history.data.history, levers), err: null };
    } catch (e) {
      if (e instanceof InvalidInputError) {
        return { ev: null, err: e };
      }
      throw e;
    }
  }, [levers, history.data]);

  // The first evaluation after a pick or a reset fixes the cases, so they
  // stay put while the user moves the other levers.
  const ev = result.ev;
  useEffect(() => {
    if (ev !== null && state.scenarios === null && ev.scenarios.defaults) {
      dispatch({
        type: "pinScenarios",
        cases: ev.scenarios.cases.map((c) => ({
          name: c.name,
          growth: c.growth,
          exitPe: c.exitPe,
        })),
      });
    }
  }, [ev, state.scenarios]);

  const p = profile.data;
  const value = useMemo(
    (): StockData => ({
      state,
      evaluatedState: evaluated,
      restored,
      profile,
      history,
      levers,
      evaluation: result.ev,
      evaluationError: result.err,
      provenance: {
        price: provenanceOf(state.price, p?.latestClose),
        ttmEps: provenanceOf(state.ttmEps, p?.ttmEps),
        dividend: provenanceOf(state.dividend, present(p?.ttmDividend)),
      },
    }),
    [state, evaluated, restored, profile, history, levers, result, p],
  );
  return (
    <DispatchContext value={dispatch}>
      <StockContext value={value}>{children}</StockContext>
    </DispatchContext>
  );
}

/** useStock returns the stock context; the values are null or undefined while data loads. */
export function useStock(): StockContextValue {
  const data = useContext(StockContext);
  const dispatch = useContext(DispatchContext);
  if (data === null || dispatch === null) {
    throw new Error("useStock needs a StockProvider");
  }
  return { ...data, dispatch };
}

/**
 * useLeverDispatch returns the dispatch of the levers alone. A component
 * that only changes levers re-renders less with it than with useStock.
 */
export function useLeverDispatch(): Dispatch<LeverAction> {
  const dispatch = useContext(DispatchContext);
  if (dispatch === null) {
    throw new Error("useLeverDispatch needs a StockProvider");
  }
  return dispatch;
}

/** ReadyStock is the stock context once the evaluation exists. Its state is the one that the evaluation was computed from. */
export interface ReadyStock {
  evaluation: Evaluation;
  levers: Levers;
  profile: Profile;
  history: StockHistory;
  state: LeverState;
  dispatch: Dispatch<LeverAction>;
  provenance: StockContextValue["provenance"];
}

/**
 * useEvaluation returns the evaluation and its inputs. Panels call it: the
 * page renders them only once an evaluation exists, so it throws when there
 * is none.
 */
export function useEvaluation(): ReadyStock {
  const s = useStock();
  if (
    s.evaluation === null ||
    s.levers === null ||
    s.profile.data === undefined ||
    s.history.data === undefined
  ) {
    throw new Error(
      "useEvaluation needs an evaluation: render panels inside the page's ready state",
    );
  }
  return {
    evaluation: s.evaluation,
    levers: s.levers,
    profile: s.profile.data,
    history: s.history.data,
    state: s.evaluatedState,
    dispatch: s.dispatch,
    provenance: s.provenance,
  };
}

/** useView returns a panel view toggle and its setter; fallback applies until it is set. */
export function useView<T extends ViewValue>(key: string, fallback: T): [T, (value: T) => void] {
  const { state, dispatch } = useStock();
  const value = (state.views[key] as T | undefined) ?? fallback;
  return [
    value,
    (v: T) => {
      dispatch({ type: "setView", key, value: v });
    },
  ];
}
