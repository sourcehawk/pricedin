import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { profileFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "@/api/testing/server";
import { defaultLevers } from "@/state/levers";
import { renderReady, renderWithStock, testLevers } from "@/testing/render";
import { StockMain } from "./StockMain";
import { TopBar } from "./TopBar";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  usePathname: () => "/",
}));
vi.mock("next/link", () => ({
  default: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

withApiServer();

beforeEach(() => {
  replace.mockReset();
});

describe("StockMain", () => {
  it("asks for a stock before the first pick", () => {
    renderWithStock(<StockMain />, { levers: defaultLevers });
    expect(screen.getByRole("heading", { name: "Pick a stock" })).toBeInTheDocument();
  });

  it("shows every panel once the evaluation exists", async () => {
    await renderReady(<StockMain />);
    for (const name of [
      "Priced in",
      "Price",
      "Return grid",
      "Earnings trajectory",
      "How demanding over time",
      "Bear, base, bull",
      "Financials",
    ]) {
      expect(screen.getByRole("region", { name })).toBeInTheDocument();
    }
  });

  it("offers to load a company that is not loaded", async () => {
    let loaded = false;
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/load"), () => {
        loaded = true;
        return HttpResponse.json(profileFixture);
      }),
    );
    renderWithStock(<StockMain />, { levers: testLevers("KO") });
    expect(
      await screen.findByRole("heading", { name: "Coca-Cola Co is not loaded yet" }),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Load this company" }));
    await waitFor(() => {
      expect(loaded).toBe(true);
    });
  });

  it("says that no company has an unknown ticker", async () => {
    renderWithStock(<StockMain />, { levers: testLevers("ZZZZ") });
    expect(
      await screen.findByRole("heading", { name: "No company has the ticker ZZZZ" }),
    ).toBeInTheDocument();
  });
});

describe("TopBar", () => {
  it("shows the company with its price, TTM EPS, P/E and close date", async () => {
    await renderReady(<TopBar />);
    const bar = screen.getByRole("banner");
    expect(bar).toHaveTextContent("Garmin Ltd.");
    expect(bar).toHaveTextContent("$270.00");
    expect(bar).toHaveTextContent("$10.80");
    expect(bar).toHaveTextContent("25.0×");
    expect(bar).toHaveTextContent(`As of close${String(profileFixture.latestClose?.asOf)}`);
  });

  it("stays and says so when the logout fails", async () => {
    apiServer.use(http.post(apiUrl("/api/auth/logout"), () => HttpResponse.error()));
    await renderReady(<TopBar />);
    await userEvent.click(screen.getByRole("button", { name: "Log out" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Logout failed. The server cannot be reached.",
    );
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Log out" })).toBeEnabled();
  });

  it("goes to the login page when the session was already gone", async () => {
    apiServer.use(http.post(apiUrl("/api/auth/logout"), () => problem(401, "Unauthenticated")));
    await renderReady(<TopBar />);
    await userEvent.click(screen.getByRole("button", { name: "Log out" }));
    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith("/login");
    });
  });

  it("logs out and goes to the login page", async () => {
    let out = false;
    apiServer.use(
      http.post(apiUrl("/api/auth/logout"), () => {
        out = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await renderReady(<TopBar />);
    await userEvent.click(screen.getByRole("button", { name: "Log out" }));
    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith("/login");
    });
    expect(out).toBe(true);
  });
});
