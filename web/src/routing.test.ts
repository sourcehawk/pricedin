import { describe, expect, it } from "vitest";
import { apiRequestHeaders, route } from "./routing";

const api = "http://server:8080";

describe("route", () => {
  it("passes an API call to the API server with its query", () => {
    expect(route({ pathname: "/api/v1/search", search: "?q=ko", hasSession: false }, api)).toEqual({
      kind: "rewrite",
      url: "http://server:8080/api/v1/search?q=ko",
    });
  });

  it("keeps the API server's own path prefix", () => {
    expect(
      route({ pathname: "/api/auth/login", search: "", hasSession: false }, "http://h/base/"),
    ).toEqual({ kind: "rewrite", url: "http://h/base/api/auth/login" });
  });

  it("sends a visitor without a session to the login page with the page as next", () => {
    expect(route({ pathname: "/data", search: "?x=1", hasSession: false }, api)).toEqual({
      kind: "redirect",
      url: "/login?next=%2Fdata%3Fx%3D1",
    });
  });

  it("lets a visitor with a session cookie through", () => {
    expect(route({ pathname: "/", search: "", hasSession: true }, api)).toEqual({ kind: "next" });
  });

  it("lets anyone open the login page", () => {
    expect(route({ pathname: "/login", search: "?next=/", hasSession: false }, api)).toEqual({
      kind: "next",
    });
  });
});

describe("apiRequestHeaders", () => {
  const secret = "0123456789abcdef0123456789abcdef";

  it("adds the secret and the address that the edge gives", () => {
    const got = apiRequestHeaders(
      new Headers({ "x-real-ip": "203.0.113.9", accept: "*/*" }),
      secret,
    );
    expect(got.get("x-pricedin-proxy")).toBe(secret);
    expect(got.get("x-pricedin-client-ip")).toBe("203.0.113.9");
    expect(got.get("accept")).toBe("*/*");
  });

  it("replaces the copies that the browser sent", () => {
    const got = apiRequestHeaders(
      new Headers({
        "x-real-ip": "203.0.113.9",
        "x-pricedin-proxy": "forged",
        "x-pricedin-client-ip": "198.51.100.7",
      }),
      secret,
    );
    expect(got.get("x-pricedin-proxy")).toBe(secret);
    expect(got.get("x-pricedin-client-ip")).toBe("203.0.113.9");
  });

  it("removes the browser's copies and adds nothing without a secret", () => {
    for (const s of [undefined, ""]) {
      const got = apiRequestHeaders(
        new Headers({
          "x-real-ip": "203.0.113.9",
          "x-pricedin-proxy": "forged",
          "x-pricedin-client-ip": "198.51.100.7",
        }),
        s,
      );
      expect(got.has("x-pricedin-proxy")).toBe(false);
      expect(got.has("x-pricedin-client-ip")).toBe(false);
    }
  });

  it("sends no address when the edge gave none", () => {
    const got = apiRequestHeaders(new Headers({ "x-pricedin-client-ip": "198.51.100.7" }), secret);
    expect(got.get("x-pricedin-proxy")).toBe(secret);
    expect(got.has("x-pricedin-client-ip")).toBe(false);
  });

  it("leaves the incoming headers as they were", () => {
    const incoming = new Headers({ "x-real-ip": "203.0.113.9", "x-pricedin-proxy": "forged" });
    apiRequestHeaders(incoming, secret);
    expect(incoming.get("x-pricedin-proxy")).toBe("forged");
  });
});
