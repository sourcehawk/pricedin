// @vitest-environment node
import { NextRequest } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { proxy } from "./proxy";

// Next carries the request headers of a rewrite to the upstream in response
// headers named x-middleware-request-<name>, and removes them before the
// response reaches the browser.
const upstream = (res: Response, name: string) => res.headers.get(`x-middleware-request-${name}`);

describe("proxy", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("sends the secret and the browser's address with an API call", () => {
    vi.stubEnv("PRICEDIN_API_URL", "https://api.example.com");
    vi.stubEnv("PRICEDIN_PROXY_SECRET", "0123456789abcdef0123456789abcdef");
    const res = proxy(
      new NextRequest("https://web.example.com/api/auth/login", {
        headers: {
          "x-real-ip": "203.0.113.9",
          "x-pricedin-proxy": "forged",
          "x-pricedin-client-ip": "198.51.100.7",
        },
      }),
    );
    expect(res.headers.get("x-middleware-rewrite")).toBe("https://api.example.com/api/auth/login");
    expect(upstream(res, "x-pricedin-proxy")).toBe("0123456789abcdef0123456789abcdef");
    expect(upstream(res, "x-pricedin-client-ip")).toBe("203.0.113.9");
  });

  it("drops the browser's copies when no secret is set", () => {
    vi.stubEnv("PRICEDIN_PROXY_SECRET", "");
    const res = proxy(
      new NextRequest("https://web.example.com/api/v1/status", {
        headers: { "x-pricedin-proxy": "forged", "x-pricedin-client-ip": "198.51.100.7" },
      }),
    );
    // Without an override, the upstream gets the browser's headers as sent.
    const kept = res.headers.get("x-middleware-override-headers")?.split(",");
    expect(kept).toBeDefined();
    expect(kept).not.toContain("x-pricedin-proxy");
    expect(kept).not.toContain("x-pricedin-client-ip");
  });
});
