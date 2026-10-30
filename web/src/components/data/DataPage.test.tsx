import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import { profileFixture, statusFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "@/api/testing/server";
import { renderWithStock } from "@/testing/render";
import { DataPage } from "./DataPage";

vi.mock("next/link", () => ({
  default: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

withApiServer();

describe("DataPage", () => {
  it("shows the freshness of each source and the coverage", async () => {
    renderWithStock(<DataPage />);
    expect(await screen.findByText(statusFixture.latestPriceDate ?? "")).toBeInTheDocument();
    expect(screen.getByText(statusFixture.latestFiled ?? "")).toBeInTheDocument();
    expect(screen.getByText(/of 3 known \(2 with facts\)/)).toBeInTheDocument();
  });

  it("lists the latest run of each job with its status and error", async () => {
    renderWithStock(<DataPage />);
    const table = await screen.findByRole("table");
    const failed = within(table).getByRole("row", { name: /sec-daily/ });
    expect(failed).toHaveTextContent("failed");
    expect(failed).toHaveTextContent("fetch daily index: 503 Service Unavailable");
    expect(within(table).getByRole("row", { name: /prices-daily/ })).toHaveTextContent(
      "bars 5,321",
    );
  });

  it("loads a company on demand", async () => {
    let loaded = "";
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/load"), ({ params }) => {
        loaded = String(params.t);
        return HttpResponse.json(profileFixture);
      }),
    );
    renderWithStock(<DataPage />);
    await userEvent.type(screen.getByLabelText("Ticker"), "aapl");
    await userEvent.click(screen.getByRole("button", { name: "Load or refresh" }));
    expect(await screen.findByText(/AAPL is loaded\./)).toBeInTheDocument();
    expect(loaded).toBe("AAPL");
  });

  it("says why a load failed", async () => {
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/load"), () => problem(404, "Unknown ticker")),
    );
    renderWithStock(<DataPage />);
    await userEvent.type(screen.getByLabelText("Ticker"), "zzzz");
    await userEvent.click(screen.getByRole("button", { name: "Load or refresh" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "ZZZZ could not be loaded. Unknown ticker",
    );
  });
});
