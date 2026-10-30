import { expect, test as base, type APIRequestContext, type Page } from "@playwright/test";
import type { components } from "../../../web/src/api/schema";
import { baseURL } from "./stack";

type Schemas = components["schemas"];
export type Profile = Schemas["StockProfile"];
export type EvaluationResponse = Schemas["EvaluationResponse"];
export type Evaluation = Schemas["Evaluation"];
export type LeversInput = Schemas["LeversInput"];
export type DataStatus = Schemas["DataStatus"];
export type SourcedNumber = Schemas["SourcedNumber"];
export type Source = Schemas["Source"];
export type SecSource = Schemas["SECSource"];

const origin = new URL(baseURL).origin;

/**
 * Api calls the API the way the page does: through the web app, with the
 * session cookie of the test's browser context.
 */
export class Api {
  constructor(private readonly request: APIRequestContext) {}

  async profile(ticker: string): Promise<Profile> {
    const r = await this.request.get(`/api/v1/stocks/${ticker}`);
    expect(r.status(), `GET profile of ${ticker}`).toBe(200);
    return (await r.json()) as Profile;
  }

  async evaluate(ticker: string, levers: LeversInput): Promise<EvaluationResponse> {
    const r = await this.request.post(`/api/v1/stocks/${ticker}/evaluation`, {
      headers: { Origin: origin },
      data: levers,
    });
    expect(r.status(), `POST evaluation of ${ticker}: ${await r.text()}`).toBe(200);
    return (await r.json()) as EvaluationResponse;
  }

  async status(): Promise<DataStatus> {
    const r = await this.request.get("/api/v1/status");
    expect(r.status(), "GET status").toBe(200);
    return (await r.json()) as DataStatus;
  }
}

/** isPhone tells the phone projects from the desktop ones. */
export function isPhone(page: Page): boolean {
  return (page.viewportSize()?.width ?? 1600) < 600;
}

export const test = base.extend<{ api: Api; stackOnly: undefined }>({
  api: async ({ page }, use) => {
    await use(new Api(page.request));
  },
  // The suite must need nothing outside the Compose stack: a request to
  // another origin fails the test.
  stackOnly: [
    async ({ context }, use) => {
      const blocked: string[] = [];
      await context.route(
        (url) => url.origin !== origin,
        (route) => {
          blocked.push(route.request().url());
          return route.abort("blockedbyclient");
        },
      );
      await use(undefined);
      expect(blocked, "requests outside the stack").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };
