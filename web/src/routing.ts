import { loginPath } from "@/api/session";

/** Decision is what the web server does with a request before it renders a page. */
export type Decision =
  { kind: "rewrite"; url: string } | { kind: "redirect"; url: string } | { kind: "next" };

/**
 * route decides a request. A call under /api/ goes to the API server at
 * apiBase on the same path, so the browser sees one origin and the session
 * cookie stays first-party. A page other than /login needs the session
 * cookie, or the visitor goes to the login page. The API still checks the
 * session on every call.
 */
export function route(
  req: { pathname: string; search: string; hasSession: boolean },
  apiBase: string,
): Decision {
  if (req.pathname === "/api" || req.pathname.startsWith("/api/")) {
    const base = apiBase.endsWith("/") ? apiBase.slice(0, -1) : apiBase;
    return { kind: "rewrite", url: `${base}${req.pathname}${req.search}` };
  }
  if (req.pathname === "/login" || req.hasSession) {
    return { kind: "next" };
  }
  return { kind: "redirect", url: loginPath(req, false) };
}
