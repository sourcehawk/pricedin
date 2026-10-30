import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import { ApiError, createApi, SessionExpiredError } from "./client";
import { exampleLevers, profileFixture } from "./testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "./testing/server";

withApiServer();

function setup() {
  const onSessionExpired = vi.fn();
  return { api: createApi({ onSessionExpired }), onSessionExpired };
}

describe("reads", () => {
  it("return the decoded body", async () => {
    const { api } = setup();
    await expect(api.profile("GRMN")).resolves.toEqual(profileFixture);
  });

  it("send the search text and the limit", async () => {
    let url = "";
    apiServer.use(
      http.get(apiUrl("/api/v1/search"), ({ request }) => {
        url = request.url;
        return HttpResponse.json({ results: [] });
      }),
    );
    const { api } = setup();
    await api.search("gar", 8);
    expect(new URL(url).searchParams.get("q")).toBe("gar");
    expect(new URL(url).searchParams.get("limit")).toBe("8");
  });
});

describe("errors", () => {
  it("turn a problem response into an ApiError with its status and title", async () => {
    const { api } = setup();
    const err = await api.profile("NOPE").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 404, problem: { title: "Unknown ticker" } });
    expect((err as ApiError).message).toBe("Unknown ticker");
  });

  it("keep the status of an error whose body is not a problem", async () => {
    apiServer.use(
      http.get(
        apiUrl("/api/v1/status"),
        () => new HttpResponse("<html>bad gateway", { status: 502 }),
      ),
    );
    const { api } = setup();
    await expect(api.status()).rejects.toMatchObject({ status: 502, problem: null });
  });

  it("report a network failure as status 0", async () => {
    apiServer.use(http.get(apiUrl("/api/v1/status"), () => HttpResponse.error()));
    const { api } = setup();
    await expect(api.status()).rejects.toMatchObject({ status: 0 });
  });
});

describe("an expired session", () => {
  it("calls onSessionExpired and throws SessionExpiredError on a 401", async () => {
    apiServer.use(http.get(apiUrl("/api/v1/stocks/:t"), () => problem(401, "Unauthorized")));
    const { api, onSessionExpired } = setup();
    await expect(api.profile("GRMN")).rejects.toBeInstanceOf(SessionExpiredError);
    expect(onSessionExpired).toHaveBeenCalledTimes(1);
  });

  it("is not reported for wrong login details", async () => {
    apiServer.use(
      http.post(apiUrl("/api/auth/login"), () => problem(401, "Wrong email or password")),
    );
    const { api, onSessionExpired } = setup();
    const err = await api.login("a@b.c", "x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).not.toBeInstanceOf(SessionExpiredError);
    expect(onSessionExpired).not.toHaveBeenCalled();
  });

  it("makes session() return null instead of an error", async () => {
    apiServer.use(http.get(apiUrl("/api/auth/session"), () => problem(401, "Unauthorized")));
    const { api, onSessionExpired } = setup();
    await expect(api.session()).resolves.toBeNull();
    expect(onSessionExpired).not.toHaveBeenCalled();
  });
});

describe("login", () => {
  it("sends the email and the password as JSON", async () => {
    let body: unknown;
    let type: string | null = null;
    apiServer.use(
      http.post(apiUrl("/api/auth/login"), async ({ request }) => {
        type = request.headers.get("Content-Type");
        body = await request.json();
        return HttpResponse.json({ email: "owner@example.com", expiresAt: "2026-11-29T00:00:00Z" });
      }),
    );
    const { api } = setup();
    await api.login("owner@example.com", "secret");
    expect(type).toMatch(/^application\/json/);
    expect(body).toEqual({ email: "owner@example.com", password: "secret" });
  });

  it("gives the seconds to wait after too many attempts", async () => {
    apiServer.use(
      http.post(apiUrl("/api/auth/login"), () =>
        problem(429, "Too many login attempts", { headers: { "Retry-After": "42" } }),
      ),
    );
    const { api } = setup();
    await expect(api.login("a@b.c", "x")).rejects.toMatchObject({ status: 429, retryAfter: 42 });
  });
});

describe("evaluation", () => {
  it("posts the levers as JSON", async () => {
    let body: unknown;
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/evaluation"), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({});
      }),
    );
    const { api } = setup();
    await api.evaluation("GRMN", exampleLevers);
    expect(body).toEqual(exampleLevers);
  });
});
