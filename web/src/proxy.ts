import { NextResponse, type NextRequest } from "next/server";
import { sessionCookie } from "@/api/session";
import { apiRequestHeaders, route } from "./routing";

// The API's base URL comes from the environment at run time, not at build
// time: one image serves Compose and production. A rewrite in next.config
// would bake the build's value into the image.
const defaultApiUrl = "http://localhost:8080";

/** proxy sends /api/* to the API server and sends a visitor without a session to the login page. */
export function proxy(request: NextRequest) {
  const d = route(
    {
      pathname: request.nextUrl.pathname,
      search: request.nextUrl.search,
      hasSession: request.cookies.has(sessionCookie),
    },
    process.env.PRICEDIN_API_URL ?? defaultApiUrl,
  );
  switch (d.kind) {
    case "rewrite":
      // The secret goes to the API server only; Next keeps request headers
      // of a rewrite out of the response.
      return NextResponse.rewrite(new URL(d.url), {
        request: {
          headers: apiRequestHeaders(request.headers, process.env.PRICEDIN_PROXY_SECRET),
        },
      });
    case "redirect":
      return NextResponse.redirect(new URL(d.url, request.url));
    case "next":
      return NextResponse.next();
  }
}

export const config = {
  // Static files and images need no session.
  matcher: ["/((?!_next/static|_next/image|favicon.ico|.*\\.(?:png|svg|ico|txt|webmanifest)$).*)"],
};
