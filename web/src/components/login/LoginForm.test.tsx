import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApi } from "@/api/client";
import { ApiProvider } from "@/api/queries";
import { apiServer, apiUrl, problem, withApiServer } from "@/api/testing/server";
import { LoginForm } from "./LoginForm";

const replace = vi.fn();
let params = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => params,
}));

withApiServer();

beforeEach(() => {
  replace.mockReset();
  params = new URLSearchParams();
});

function renderForm() {
  render(
    <ApiProvider api={createApi()}>
      <LoginForm />
    </ApiProvider>,
  );
}

async function logIn(email = "owner@example.com", password = "secret") {
  await userEvent.type(screen.getByLabelText("Email"), email);
  await userEvent.type(screen.getByLabelText("Password"), password);
  await userEvent.click(screen.getByRole("button", { name: "Log in" }));
}

describe("LoginForm", () => {
  it("goes to the page in next after a login", async () => {
    params = new URLSearchParams({ next: "/data" });
    renderForm();
    await logIn();
    expect(replace).toHaveBeenCalledWith("/data");
  });

  it("goes home when next points to another site", async () => {
    params = new URLSearchParams({ next: "https://evil.example" });
    renderForm();
    await logIn();
    expect(replace).toHaveBeenCalledWith("/");
  });

  it("says that the email or password is wrong", async () => {
    apiServer.use(http.post(apiUrl("/api/auth/login"), () => problem(401, "Unauthorized")));
    renderForm();
    await logIn();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The email or the password is wrong.",
    );
    expect(replace).not.toHaveBeenCalled();
  });

  it("says how long to wait after too many attempts", async () => {
    apiServer.use(
      http.post(apiUrl("/api/auth/login"), () =>
        problem(429, "Too many requests", { headers: { "Retry-After": "40" } }),
      ),
    );
    renderForm();
    await logIn();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 40 seconds.",
    );
  });

  it("says when the server cannot be reached", async () => {
    apiServer.use(http.post(apiUrl("/api/auth/login"), () => HttpResponse.error()));
    renderForm();
    await logIn();
    expect(await screen.findByRole("alert")).toHaveTextContent("The server cannot be reached.");
  });

  it("disables the button while it logs in", async () => {
    let release: () => void = () => undefined;
    apiServer.use(
      http.post(
        apiUrl("/api/auth/login"),
        () =>
          new Promise<Response>((resolve) => {
            release = () => {
              resolve(HttpResponse.json({ email: "o@e.c", expiresAt: "2026-11-29T00:00:00Z" }));
            };
          }),
      ),
    );
    renderForm();
    await logIn();
    expect(screen.getByRole("button", { name: "Logging in…" })).toBeDisabled();
    release();
    await vi.waitFor(() => {
      expect(replace).toHaveBeenCalled();
    });
  });

  it("notes that the session ended", () => {
    params = new URLSearchParams({ expired: "1", next: "/" });
    renderForm();
    expect(screen.getByRole("status")).toHaveTextContent("Your session ended. Log in again.");
  });

  it("needs an email and a password", () => {
    renderForm();
    expect(screen.getByLabelText("Email")).toBeRequired();
    expect(screen.getByLabelText("Password")).toBeRequired();
  });
});
