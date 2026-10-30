import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { profileFixture } from "@/api/testing/fixtures";
import { apiServer, apiUrl, problem, withApiServer } from "@/api/testing/server";
import { useStock } from "@/state/StockProvider";
import { renderReady } from "@/testing/render";
import { StockSearch } from "./StockSearch";

withApiServer();

function Ticker() {
  return <output aria-label="ticker">{useStock().state.ticker}</output>;
}

async function setup() {
  await renderReady(
    <>
      <StockSearch inputId="pick" />
      <Ticker />
    </>,
  );
  return screen.getByRole("combobox", { name: "Pick a stock" });
}

const options = () => screen.getAllByRole("option");
const selected = () => options().find((o) => o.getAttribute("aria-selected") === "true");

describe("StockSearch", () => {
  it("shows the selected ticker until it gets focus", async () => {
    const box = await setup();
    expect(box).toHaveValue("GRMN");
    expect(box).toHaveAttribute("aria-expanded", "false");
  });

  it("opens the list on focus and asks for a ticker or a name", async () => {
    const box = await setup();
    await userEvent.click(box);
    expect(box).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("listbox")).toHaveTextContent("Type a ticker or a company name.");
  });

  it("lists the matches with the first one active", async () => {
    const box = await setup();
    await userEvent.type(box, "g");
    await waitFor(() => {
      expect(options()).toHaveLength(3);
    });
    expect(selected()).toHaveTextContent("GRMN");
    expect(box).toHaveAttribute("aria-activedescendant", selected()?.id);
  });

  it("moves the active option with the arrow keys and wraps around", async () => {
    const box = await setup();
    await userEvent.type(box, "g");
    await waitFor(() => {
      expect(options()).toHaveLength(3);
    });
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()).toHaveTextContent("GOOGL");
    await userEvent.keyboard("{ArrowUp}{ArrowUp}");
    expect(selected()).toHaveTextContent("GE");
    expect(box).toHaveAttribute("aria-activedescendant", selected()?.id);
  });

  it("picks the active stock on Enter", async () => {
    const box = await setup();
    await userEvent.type(box, "ge");
    await waitFor(() => {
      expect(options()).toHaveLength(1);
    });
    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GE");
    expect(box).toHaveAttribute("aria-expanded", "false");
  });

  it("closes on Escape without picking", async () => {
    const box = await setup();
    await userEvent.type(box, "ge");
    await userEvent.keyboard("{Escape}");
    expect(box).toHaveAttribute("aria-expanded", "false");
    expect(box).toHaveValue("GRMN");
  });

  it("says when nothing matches", async () => {
    const box = await setup();
    await userEvent.type(box, "zzz");
    expect(await screen.findByText(/^No match\./)).toBeInTheDocument();
  });

  it("offers to load a company that is not loaded and selects it once loaded", async () => {
    let loaded = "";
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/load"), ({ params }) => {
        loaded = String(params.t);
        return HttpResponse.json(profileFixture);
      }),
    );
    const box = await setup();
    await userEvent.type(box, "goo");
    const option = await screen.findByRole("option", { name: /GOOGL/ });
    expect(option).toHaveTextContent("Load this company");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => {
      expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GOOGL");
    });
    expect(loaded).toBe("GOOGL");
  });

  it("keeps a newer pick when an older load finishes later", async () => {
    let finish: () => void = () => undefined;
    apiServer.use(
      http.post(
        apiUrl("/api/v1/stocks/:t/load"),
        () =>
          new Promise<Response>((resolve) => {
            finish = () => {
              resolve(HttpResponse.json(profileFixture));
            };
          }),
      ),
    );
    const box = await setup();
    await userEvent.type(box, "goo");
    await screen.findByRole("option", { name: /GOOGL/ });
    await userEvent.keyboard("{Enter}");
    await userEvent.type(box, "ge");
    await userEvent.click(await screen.findByRole("option", { name: /GE Aerospace/ }));
    expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GE");
    finish();
    await waitFor(() => {
      expect(screen.queryByText(/Loading GOOGL/)).not.toBeInTheDocument();
    });
    expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GE");
  });

  it("shows why a load failed and keeps the stock", async () => {
    apiServer.use(
      http.post(apiUrl("/api/v1/stocks/:t/load"), () =>
        problem(501, "Loading a company is not available in this build"),
      ),
    );
    const box = await setup();
    await userEvent.type(box, "goo");
    await screen.findByRole("option", { name: /GOOGL/ });
    await userEvent.keyboard("{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "GOOGL could not be loaded. Loading a company is not available in this build",
    );
    expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GRMN");
  });

  it("picks a stock with the mouse", async () => {
    const box = await setup();
    await userEvent.type(box, "ge");
    await userEvent.click(await screen.findByRole("option", { name: /GE Aerospace/ }));
    expect(screen.getByRole("status", { name: "ticker" })).toHaveTextContent("GE");
  });
});
