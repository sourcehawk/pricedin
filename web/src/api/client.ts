import createClient from "openapi-fetch";
import type { paths } from "./schema";
import type {
  EvaluationResult,
  Fundamentals,
  LeversInput,
  Problem,
  Profile,
  Session,
  Status,
  StockHistory,
  TickerMatch,
} from "./types";

/** ApiError is a failed API call. status is 0 when the server could not be reached. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    /** The RFC 9457 problem of the response; null when the body is not one. */
    readonly problem: Problem | null,
    /** Seconds from the Retry-After header; null without one. */
    readonly retryAfter: number | null,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** SessionExpiredError is a 401 from a call that needs a session. */
export class SessionExpiredError extends ApiError {
  constructor(problem: Problem | null) {
    super(401, problem, null, "Your session ended.");
    this.name = "SessionExpiredError";
  }
}

/** HistoryRange bounds the daily closes of a history. */
export type HistoryRange = "6M" | "1Y" | "3Y" | "max";

/**
 * Api calls the PricedIn HTTP API. Every method rejects with an ApiError
 * on failure. A 401 from a call that needs a session calls
 * onSessionExpired, then rejects with a SessionExpiredError.
 */
export interface Api {
  /** Rejects with status 401 for a wrong email or password and 429 after too many attempts. */
  login(email: string, password: string): Promise<Session>;
  logout(): Promise<void>;
  /** null when there is no valid session. */
  session(): Promise<Session | null>;
  search(q: string, limit?: number): Promise<TickerMatch[]>;
  profile(ticker: string): Promise<Profile>;
  history(ticker: string, range?: HistoryRange): Promise<StockHistory>;
  fundamentals(ticker: string): Promise<Fundamentals>;
  /** Fields left out of levers take the stock's defaults on the server. */
  evaluation(ticker: string, levers: LeversInput): Promise<EvaluationResult>;
  /** Loads or refreshes one company; resolves with its profile when the data is stored. */
  load(ticker: string): Promise<Profile>;
  status(): Promise<Status>;
}

export interface ApiOptions {
  /** Defaults to the page's origin, so the session cookie stays first-party. */
  baseUrl?: string;
  onSessionExpired?: () => void;
}

function isProblem(v: unknown): v is Problem {
  return (
    typeof v === "object" &&
    v !== null &&
    typeof (v as Problem).title === "string" &&
    typeof (v as Problem).status === "number"
  );
}

function retryAfter(r: Response): number | null {
  const v = r.headers.get("Retry-After");
  if (v === null) {
    return null;
  }
  const n = Number(v);
  return Number.isFinite(n) && n >= 0 ? n : null;
}

type Result = Promise<{ data?: unknown; error?: unknown; response: Response }>;

/** createApi returns an Api client. */
export function createApi(opts: ApiOptions = {}): Api {
  const baseUrl = opts.baseUrl ?? (typeof window === "undefined" ? "" : window.location.origin);
  // Calls the global fetch at request time, so a test's interceptor applies.
  const client = createClient<paths>({ baseUrl, fetch: (r) => globalThis.fetch(r) });
  // The contract declares the Origin header of a cookie POST. A browser
  // sends its own and ignores this one; other clients send this one.
  const origin = { Origin: baseUrl === "" ? "" : new URL(baseUrl).origin };

  // The types of the responses come from the Api interface: openapi-fetch's
  // own response types turn readonly arrays of the engine types into objects.
  async function call<T>(request: () => Result, auth: "session" | "login"): Promise<T> {
    let res: Awaited<Result>;
    try {
      res = await request();
    } catch {
      throw new ApiError(0, null, null, "The server cannot be reached. Check your connection.");
    }
    const { response } = res;
    if (response.ok) {
      return res.data as T;
    }
    const problem = isProblem(res.error) ? res.error : null;
    if (response.status === 401 && auth === "session") {
      opts.onSessionExpired?.();
      throw new SessionExpiredError(problem);
    }
    throw new ApiError(
      response.status,
      problem,
      retryAfter(response),
      problem?.detail ?? problem?.title ?? `The server answered ${String(response.status)}.`,
    );
  }

  return {
    login: (email, password) =>
      call<Session>(
        () =>
          client.POST("/api/auth/login", { params: { header: origin }, body: { email, password } }),
        "login",
      ),
    logout: async () => {
      await call<undefined>(
        () => client.POST("/api/auth/logout", { params: { header: origin }, body: {} }),
        "login",
      );
    },
    session: async () => {
      try {
        return await call<Session>(() => client.GET("/api/auth/session"), "login");
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          return null;
        }
        throw e;
      }
    },
    search: async (q, limit) => {
      const query = limit === undefined ? { q } : { q, limit };
      const r = await call<{ results: TickerMatch[] }>(
        () => client.GET("/api/v1/search", { params: { query } }),
        "session",
      );
      return r.results;
    },
    profile: (ticker) =>
      call<Profile>(
        () => client.GET("/api/v1/stocks/{ticker}", { params: { path: { ticker } } }),
        "session",
      ),
    history: (ticker, range = "3Y") =>
      call<StockHistory>(
        () =>
          client.GET("/api/v1/stocks/{ticker}/history", {
            params: { path: { ticker }, query: { range } },
          }),
        "session",
      ),
    fundamentals: (ticker) =>
      call<Fundamentals>(
        () => client.GET("/api/v1/stocks/{ticker}/fundamentals", { params: { path: { ticker } } }),
        "session",
      ),
    evaluation: (ticker, levers) =>
      call<EvaluationResult>(
        () =>
          client.POST("/api/v1/stocks/{ticker}/evaluation", {
            params: { path: { ticker }, header: origin },
            body: levers,
          }),
        "session",
      ),
    load: (ticker) =>
      call<Profile>(
        () =>
          client.POST("/api/v1/stocks/{ticker}/load", {
            params: { path: { ticker }, header: origin },
            body: {},
          }),
        "session",
      ),
    status: () => call<Status>(() => client.GET("/api/v1/status"), "session"),
  };
}
