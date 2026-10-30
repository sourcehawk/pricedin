// The facts of the stack that scripts/e2e.sh starts.

import { randomBytes } from "node:crypto";
import path from "node:path";

/** baseURL is the web app; the API answers below it at /api, as for a browser. */
export const baseURL = process.env.PRICEDIN_E2E_BASE_URL ?? "http://localhost:3000";

/** apiURL is the API server itself, without the web app in front. */
export const apiURL = process.env.PRICEDIN_E2E_API_URL ?? "http://localhost:8080";

export const owner = {
  email: process.env.PRICEDIN_E2E_OWNER_EMAIL ?? "e2e-owner@example.com",
  password: process.env.PRICEDIN_E2E_OWNER_PASSWORD ?? "e2e-owner-password",
};

/** ownerState is the file that holds the owner's session for every test that needs one. */
export const ownerState = path.join(import.meta.dirname, "..", "..", ".auth", "owner.json");

/**
 * newClientIP returns a random client address, so that two logins, also of
 * runs a minute apart, almost never share one. The server limits logins
 * per client address. The
 * stack's web app passes the x-real-ip header on to the server as that
 * address, as Vercel's edge does in production, so a test that sets it
 * has a rate limit of its own.
 */
export function newClientIP(): string {
  const [a = 0, b = 0, c = 0] = randomBytes(3);
  return `10.${String(a)}.${String(b)}.${String(c)}`;
}

/**
 * fixture lists the fixture companies of the seed. The seed leaves `unloaded`
 * out (docker-compose.e2e.yml), so that a test can load it on demand.
 */
export const fixture = {
  loaded: ["NVDA", "AAPL", "KO", "GRMN"],
  unloaded: "ADM",
} as const;
