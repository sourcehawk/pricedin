import { describe, expect, it } from "vitest";
import { loginPath, safeNext, sessionCookie } from "./session";

describe("safeNext", () => {
  it("keeps a path on this site with its query", () => {
    expect(safeNext("/data?x=1")).toBe("/data?x=1");
  });

  it("gives back the parsed path, query and fragment, not the raw text", () => {
    expect(safeNext("/data/../data?x=1#runs")).toBe("/data?x=1#runs");
    expect(safeNext("/a b")).toBe("/a%20b");
  });

  it("goes home for a missing or empty value", () => {
    expect(safeNext(null)).toBe("/");
    expect(safeNext("")).toBe("/");
  });

  it("goes home for a URL that leaves the site", () => {
    expect(safeNext("https://evil.example/")).toBe("/");
    expect(safeNext("//evil.example/")).toBe("/");
    expect(safeNext("/\\evil.example/")).toBe("/");
  });

  it("goes home for a path whose dot segments normalize into another site", () => {
    expect(safeNext("/.//evil.example")).toBe("/");
    expect(safeNext("/a/..//evil.example/x")).toBe("/");
  });

  it("goes home for a path that a URL parser turns into another site", () => {
    expect(safeNext("/\n/evil.example")).toBe("/");
    expect(safeNext("/\t/evil.example")).toBe("/");
    expect(safeNext("/\u0000x")).toBe("/");
  });

  it("goes home for the login page itself", () => {
    expect(safeNext("/login?next=/")).toBe("/");
  });
});

describe("loginPath", () => {
  it("sends the current page as next", () => {
    expect(loginPath({ pathname: "/data", search: "?a=1" }, false)).toBe(
      "/login?next=%2Fdata%3Fa%3D1",
    );
  });

  it("adds the session-ended note", () => {
    expect(loginPath({ pathname: "/", search: "" }, true)).toBe("/login?next=%2F&expired=1");
  });
});

describe("sessionCookie", () => {
  it("is the API's session cookie", () => {
    expect(sessionCookie).toBe("pricedin_session");
  });
});
