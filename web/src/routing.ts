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

// The API trusts the address in clientIPHeader only on a request whose
// secretHeader holds the deploy secret: server/internal/auth.ProxyClientIP.
const secretHeader = "x-pricedin-proxy";
const clientIPHeader = "x-pricedin-client-ip";
// Vercel's edge sets x-real-ip to the address of the connection and
// overwrites any value the browser sent. Next itself sets no such header.
const edgeClientIPHeader = "x-real-ip";

/**
 * apiRequestHeaders returns the headers of an API call as the API server
 * gets them: incoming without the browser's copies of the two proxy
 * headers, plus the secret and the browser's address when secret is set.
 * Set secret only where Vercel's edge sets x-real-ip: elsewhere a browser
 * can choose that value. incoming is not changed.
 */
export function apiRequestHeaders(incoming: Headers, secret: string | undefined): Headers {
  const headers = new Headers(incoming);
  headers.delete(secretHeader);
  headers.delete(clientIPHeader);
  if (secret) {
    headers.set(secretHeader, secret);
    const ip = incoming.get(edgeClientIPHeader);
    if (ip) {
      headers.set(clientIPHeader, ip);
    }
  }
  return headers;
}
