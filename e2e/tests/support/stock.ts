import type { Locator, Page } from "@playwright/test";
import { expect, type Api, type Evaluation, type LeversInput } from "./fixtures";
import { pct, pp, usd } from "./format";

/**
 * SavedLevers is the lever state that the page keeps in localStorage under
 * "pricedin.levers" (web/src/state/storage.ts). The tests read it to call
 * the API with the levers that the page shows.
 */
interface SavedLevers {
  ticker: string | null;
  price: number | null;
  ttmEps: number | null;
  dividend: number | null;
  targetReturn: number;
  holdingYears: number;
  exitPe: {
    mode: "estimated" | "today" | "trailing" | "custom";
    custom: number;
    trailingMonths: number;
  };
  baseline: {
    mode: "weighted" | "today" | "trailing" | "custom";
    trailingMonths: number;
    yearly: boolean;
    rate: number;
    rates: number[];
  };
  scenarios: { name: string; growth: number; exitPe: number }[] | null;
  chartRange: "6M" | "1Y" | "3Y";
}

/** savedLevers returns the page's lever state. */
export async function savedLevers(page: Page): Promise<SavedLevers> {
  const raw = await page.evaluate(() => localStorage.getItem("pricedin.levers"));
  expect(raw, "the page saves its levers").not.toBeNull();
  return (JSON.parse(raw ?? "{}") as { state: SavedLevers }).state;
}

/**
 * leversInput returns the evaluation request for the page's levers. A
 * starting point that the user did not type is left out, so the API fills
 * in the stock's own value, as the page does.
 */
export function leversInput(s: SavedLevers): LeversInput {
  return {
    ...(s.price === null ? {} : { price: s.price }),
    ...(s.ttmEps === null ? {} : { ttmEps: s.ttmEps }),
    ...(s.dividend === null ? {} : { dividend: s.dividend }),
    targetReturn: s.targetReturn,
    holdingYears: s.holdingYears,
    exitPe: {
      mode: s.exitPe.mode,
      custom: s.exitPe.custom,
      trailingMonths: s.exitPe.trailingMonths,
    },
    baseline: {
      mode: s.baseline.mode,
      trailingMonths: s.baseline.trailingMonths,
      custom: { yearly: s.baseline.yearly, rate: s.baseline.rate, rates: s.baseline.rates },
    },
    ...(s.scenarios === null ? {} : { scenarios: s.scenarios }),
    chartRange: s.chartRange,
  };
}

/** evaluateAsPage evaluates the page's stock with the page's levers through the API. */
export async function evaluateAsPage(page: Page, api: Api): Promise<Evaluation> {
  const s = await savedLevers(page);
  expect(s.ticker, "a stock is picked").not.toBeNull();
  return (await api.evaluate(s.ticker ?? "", leversInput(s))).evaluation;
}

/**
 * pickStock types query into the stock search, selects the option of
 * ticker with the arrow keys and Enter, and waits for the priced-in panel.
 */
export async function pickStock(page: Page, query: string, ticker: string): Promise<void> {
  const box = page.getByRole("combobox", { name: "Pick a stock" });
  await box.click();
  await box.pressSequentially(query);
  const options = page.getByRole("listbox").getByRole("option");
  const wanted = options.filter({
    has: page.locator(".s", { hasText: new RegExp(`^${ticker}$`) }),
  });
  await expect(wanted).toHaveCount(1);
  const symbols = await options.locator(".s").allInnerTexts();
  const index = symbols.indexOf(ticker);
  for (let i = 0; i < index; i++) {
    await box.press("ArrowDown");
  }
  await expect(wanted).toHaveAttribute("aria-selected", "true");
  await box.press("Enter");
  await expect(pricedInPanel(page)).toBeVisible();
  await expect(page.locator(".top .co b")).not.toBeEmpty();
}

export const pricedInPanel = (page: Page): Locator => page.locator('[data-panel="priced-in"]');

/** kv returns the value cell of the priced-in panel's row whose term starts with term. */
function kv(panel: Locator, term: string): Locator {
  return panel
    .locator("dl.kv dt", { hasText: new RegExp(`^${term}`) })
    .locator("xpath=following-sibling::dd[1]");
}

/** PricedInText is the text of the priced-in numbers, as the page shows them. */
export interface PricedInText {
  required: string;
  exitPrice: string | null;
  epsNeeded: string | null;
  verdict: string | null;
  weighted: string;
  today: string;
  trailing: string;
  gap: string;
}

/** pricedInOnPage reads the priced-in numbers from the page. */
export async function pricedInOnPage(page: Page): Promise<PricedInText> {
  const panel = pricedInPanel(page);
  const exit = panel.locator(".vexit > div");
  const hasExit = (await exit.count()) > 0;
  const pill = panel.locator('[data-walk="verdict"]');
  return {
    required: (await panel.getByTestId("required-growth").innerText()).replace(/\s*\/\s*yr$/, ""),
    exitPrice: hasExit ? await exit.nth(0).locator("b").innerText() : null,
    epsNeeded: hasExit ? await exit.nth(1).locator("b").innerText() : null,
    verdict: (await pill.count()) > 0 ? ((await pill.innerText()).split(":")[0] ?? null) : null,
    weighted: await kv(panel, "Weighted trend").innerText(),
    today: await kv(panel, "Today").innerText(),
    trailing: await kv(panel, "Trailing growth").innerText(),
    gap: await kv(panel, "Gap vs").innerText(),
  };
}

const verdictWord = { undemanding: "Undemanding", tight: "Tight", demanding: "Demanding" } as const;

/** pricedInExpected formats an API evaluation as the priced-in panel shows it. */
export function pricedInExpected(ev: Evaluation): PricedInText {
  const R = ev.required.growth.value;
  const none = R === null && ev.required.growth.reason === "dividends_reach_target";
  const v = ev.verdict.value;
  return {
    required: R !== null ? pct(R) : none ? "none" : "n/a",
    exitPrice: R === null ? null : usd(ev.required.exitPrice.value),
    epsNeeded: R === null ? null : usd(ev.required.eps.value),
    verdict: v === null ? null : verdictWord[v],
    weighted: pct(ev.baselines.weighted.value),
    today: pct(ev.baselines.today.value),
    trailing: pct(ev.baselines.trailing.growth.value),
    gap: pp(ev.gap.value),
  };
}

/**
 * expectPricedInMatchesApi waits until the priced-in panel shows the API's
 * evaluation of the page's levers. On a timeout, the failure shows both.
 */
export async function expectPricedInMatchesApi(page: Page, api: Api): Promise<void> {
  await expect
    .poll(
      async () => {
        const want = pricedInExpected(await evaluateAsPage(page, api));
        const shown = await pricedInOnPage(page);
        return JSON.stringify(shown) === JSON.stringify(want) ? "equal" : { shown, api: want };
      },
      { message: "the priced-in panel equals the API evaluation of the page's levers" },
    )
    .toBe("equal");
}
