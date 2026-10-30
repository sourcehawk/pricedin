import { describe, expect, it } from "vitest";
import { route } from "./routing";

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
