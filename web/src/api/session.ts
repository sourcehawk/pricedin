/** sessionCookie is the name of the API's session cookie. The route guard checks only that it exists; the API validates it. */
export const sessionCookie = "pricedin_session";

/**
 * safeNext returns next when it is a path on this site other than the
 * login page, and "/" otherwise, so a login link cannot send the owner to
 * another site.
 */
export function safeNext(next: string | null | undefined): string {
  // A URL parser drops tabs and newlines, so "/\n/x" would become "//x".
  if (
    next === null ||
    next === undefined ||
    !next.startsWith("/") ||
    next.startsWith("//") ||
    next.startsWith("/\\") ||
    /[\u0000-\u001f\u007f]/.test(next)
  ) {
    return "/";
  }
  const base = "http://pricedin.invalid";
  const url = new URL(next, base);
  // Dot segments can normalize "/.//x" into "//x", which names another host.
  if (
    url.origin !== base ||
    url.pathname.startsWith("//") ||
    url.pathname === "/login" ||
    url.pathname.startsWith("/login/")
  ) {
    return "/";
  }
  return url.pathname + url.search + url.hash;
}

/** loginPath is the login page that returns to the page at where; expired adds the note that the session ended. */
export function loginPath(where: { pathname: string; search: string }, expired: boolean): string {
  const q = new URLSearchParams({ next: where.pathname + where.search });
  if (expired) {
    q.set("expired", "1");
  }
  return `/login?${q.toString()}`;
}
