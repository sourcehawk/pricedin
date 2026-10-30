import { http, HttpResponse, type HttpHandler } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll } from "vitest";
import type { Problem } from "../types";
import {
  historyFixture,
  notLoadedProfile,
  profileFixture,
  searchFixture,
  statusFixture,
} from "./fixtures";

/** apiUrl is the absolute URL of an API path, on the page's origin as the browser calls it. */
export function apiUrl(path: string): string {
  return new URL(path, window.location.origin).toString();
}

/** problem is an RFC 9457 problem response. */
export function problem(status: number, title: string, init?: ResponseInit): HttpResponse<Problem> {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/problem+json");
  return HttpResponse.json({ title, status }, { ...init, status, headers });
}

/** defaultHandlers answer every endpoint with the fixtures, for a logged-in owner. */
export const defaultHandlers: HttpHandler[] = [
  http.post(apiUrl("/api/auth/login"), () =>
    HttpResponse.json({ email: "owner@example.com", expiresAt: "2026-11-29T00:00:00Z" }),
  ),
  http.post(apiUrl("/api/auth/logout"), () => new HttpResponse(null, { status: 204 })),
  http.get(apiUrl("/api/auth/session"), () =>
    HttpResponse.json({ email: "owner@example.com", expiresAt: "2026-11-29T00:00:00Z" }),
  ),
  http.get(apiUrl("/api/v1/search"), ({ request }) => {
    const q = (new URL(request.url).searchParams.get("q") ?? "").toLowerCase();
    const results = searchFixture.filter(
      (m) => m.symbol.toLowerCase().startsWith(q) || m.name.toLowerCase().includes(q),
    );
    return HttpResponse.json({ results });
  }),
  http.get(apiUrl("/api/v1/stocks/:ticker"), ({ params }) => {
    if (params.ticker === "KO") {
      return HttpResponse.json(notLoadedProfile);
    }
    if (params.ticker !== "GRMN") {
      return problem(404, "Unknown ticker");
    }
    return HttpResponse.json(profileFixture);
  }),
  http.get(apiUrl("/api/v1/stocks/:ticker/history"), ({ params }) =>
    params.ticker === "GRMN"
      ? HttpResponse.json(historyFixture)
      : problem(409, "Company data not loaded"),
  ),
  http.post(apiUrl("/api/v1/stocks/:ticker/load"), () => HttpResponse.json(profileFixture)),
  http.get(apiUrl("/api/v1/status"), () => HttpResponse.json(statusFixture)),
];

/** apiServer intercepts fetch in tests; withApiServer starts it for one test file. */
export const apiServer = setupServer(...defaultHandlers);

/** withApiServer starts apiServer for the test file and fails on a request that no handler answers. */
export function withApiServer(): void {
  beforeAll(() => {
    apiServer.listen({ onUnhandledFrame: "error" });
  });
  afterEach(() => {
    apiServer.resetHandlers();
  });
  afterAll(() => {
    apiServer.close();
  });
}
