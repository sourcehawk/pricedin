import type { Locator, Page } from "@playwright/test";
import { expect, test, type Api } from "./support/fixtures";
import { expectPricedInMatchesApi, pickStock, savedLevers } from "./support/stock";

const rail = (page: Page): Locator => page.getByRole("complementary", { name: "Assumptions" });
const lever = (page: Page, key: string): Locator => rail(page).locator(`[data-lever="${key}"]`);

/** enter replaces the text of a number field and leaves it, as a user does. */
async function enter(field: Locator, text: string): Promise<void> {
  await field.fill(text);
  await field.blur();
}

/** press clicks a button of a segmented switch and expects it to be the pressed one. */
async function press(group: Locator, name: string): Promise<void> {
  const button = group.getByRole("button", { name, exact: true });
  await button.click();
  await expect(button).toHaveAttribute("aria-pressed", "true");
}

async function open(page: Page, query: string, ticker: string, api: Api): Promise<void> {
  await page.goto("/");
  await pickStock(page, query, ticker);
  await expectPricedInMatchesApi(page, api);
}

test("the target return, the holding period and the starting point change the requirement as the API computes it", async ({
  page,
  api,
}) => {
  await open(page, "GR", "GRMN", api);
  const r = rail(page);

  await lever(page, "targetReturn").locator('input[type="range"]').fill("10");
  expect((await savedLevers(page)).targetReturn).toBeCloseTo(0.1, 9);
  await expectPricedInMatchesApi(page, api);
  await enter(r.getByLabel("Target return, percent a year"), "17.5");
  expect((await savedLevers(page)).targetReturn).toBeCloseTo(0.175, 9);
  await expectPricedInMatchesApi(page, api);

  await r.getByRole("slider", { name: "Holding period" }).fill("7");
  expect((await savedLevers(page)).holdingYears).toBe(7);
  await expectPricedInMatchesApi(page, api);
  await enter(r.getByLabel("Holding period, years"), "2");
  expect((await savedLevers(page)).holdingYears).toBe(2);
  await expectPricedInMatchesApi(page, api);
  await press(r.getByRole("group", { name: "Holding period" }), "5Y");
  await expectPricedInMatchesApi(page, api);

  await enter(r.getByLabel("Share price ($)"), "150");
  await enter(r.getByLabel("TTM EPS ($)"), "6.5");
  await enter(r.getByLabel("Dividend per share, yearly ($)"), "4");
  expect(await savedLevers(page)).toMatchObject({ price: 150, ttmEps: 6.5, dividend: 4 });
  await expect(r.getByText("Edited by you.")).toBeVisible();
  await expectPricedInMatchesApi(page, api);
  // An empty field goes back to the stock's own value.
  await enter(r.getByLabel("Share price ($)"), "");
  expect((await savedLevers(page)).price).toBeNull();
  await expectPricedInMatchesApi(page, api);
});

test("every exit P/E mode, the custom value and the typical P/E look-back give the API's requirement", async ({
  page,
  api,
}) => {
  await open(page, "NV", "NVDA", api);
  const modes = lever(page, "exitPe").getByRole("group", { name: "Exit P/E" });

  for (const mode of ["Today", "Trailing", "Custom", "Estimated"]) {
    await press(modes, mode);
    expect((await savedLevers(page)).exitPe.mode).toBe(mode.toLowerCase());
    await expectPricedInMatchesApi(page, api);
  }

  await enter(lever(page, "typicalPeMonths").locator("input"), "24");
  expect((await savedLevers(page)).exitPe.trailingMonths).toBe(24);
  await press(modes, "Trailing");
  await expectPricedInMatchesApi(page, api);

  // The slider and the text field both set a custom exit P/E.
  await lever(page, "exitPe").getByLabel("Exit P/E multiple", { exact: true }).fill("18");
  expect((await savedLevers(page)).exitPe).toMatchObject({ mode: "custom", custom: 18 });
  await expectPricedInMatchesApi(page, api);
  await enter(lever(page, "exitPe").getByLabel("Exit P/E multiple, type a number"), "31.5");
  expect((await savedLevers(page)).exitPe).toMatchObject({ mode: "custom", custom: 31.5 });
  await expectPricedInMatchesApi(page, api);
});

test("every growth baseline mode, custom rates year by year and the trajectory fill give the API's numbers", async ({
  page,
  api,
}) => {
  await open(page, "GR", "GRMN", api);
  const baseline = lever(page, "baseline");
  const modes = baseline.getByRole("group", { name: "Growth baseline" });

  for (const mode of ["Today", "Trailing", "Weighted"]) {
    await press(modes, mode);
    expect((await savedLevers(page)).baseline.mode).toBe(mode.toLowerCase());
    await expectPricedInMatchesApi(page, api);
  }
  await enter(lever(page, "trailingMonths").locator("input"), "24");
  expect((await savedLevers(page)).baseline.trailingMonths).toBe(24);
  await expectPricedInMatchesApi(page, api);

  // The trajectory fill turns the weighted trend into custom rates, one per year.
  await baseline.getByRole("button", { name: "Follow this trend year by year" }).click();
  const filled = (await savedLevers(page)).baseline;
  expect(filled).toMatchObject({ mode: "custom", yearly: true });
  expect(filled.rates).toHaveLength(3);
  await expect(baseline.getByText("Filled from the weighted trend’s trajectory.")).toBeVisible();
  await expectPricedInMatchesApi(page, api);

  await enter(baseline.getByLabel("EPS growth in year 2, percent"), "4");
  const own = (await savedLevers(page)).baseline;
  expect(own.rates[1]).toBeCloseTo(0.04, 9);
  expect(own.rates[0]).toBeCloseTo(filled.rates[0] ?? Number.NaN, 9);
  await expect(baseline.getByText("Your own rates.")).toBeVisible();
  await expectPricedInMatchesApi(page, api);

  await baseline.getByRole("button", { name: "Refill from weighted trend" }).click();
  expect((await savedLevers(page)).baseline.rates).toEqual(filled.rates);
  await expectPricedInMatchesApi(page, api);

  await press(baseline.getByRole("group", { name: "Custom growth" }), "Same each year");
  await baseline.getByLabel("Custom EPS growth").fill("22");
  expect((await savedLevers(page)).baseline).toMatchObject({ yearly: false, rate: 0.22 });
  await expectPricedInMatchesApi(page, api);

  // Typing into the baseline field sets a flat custom rate from any mode.
  await press(modes, "Weighted");
  await enter(
    baseline.getByLabel("Growth baseline, percent a year; typing sets a custom rate"),
    "9.5",
  );
  expect((await savedLevers(page)).baseline).toMatchObject({
    mode: "custom",
    yearly: false,
    rate: 0.095,
  });
  await expectPricedInMatchesApi(page, api);
});

test("Reset inputs restores every default, and a new stock keeps only the target return and the holding period", async ({
  page,
  api,
}) => {
  await open(page, "NV", "NVDA", api);
  const r = rail(page);
  const exitModes = r.getByRole("group", { name: "Exit P/E" });
  const baselineModes = r.getByRole("group", { name: "Growth baseline" });
  const changeEverything = async () => {
    await enter(r.getByLabel("Target return, percent a year"), "12");
    await enter(r.getByLabel("Holding period, years"), "5");
    await enter(r.getByLabel("Share price ($)"), "200");
    await press(exitModes, "Today");
    await press(baselineModes, "Trailing");
  };
  const expectDefaultsFor = async (ticker: string, target: string, years: string) => {
    const close = (await api.profile(ticker)).latestClose?.value;
    await expect(r.getByLabel("Target return, percent a year")).toHaveValue(target);
    await expect(r.getByLabel("Holding period, years")).toHaveValue(years);
    await expect(r.getByLabel("Share price ($)")).toHaveValue(String(close));
    await expect(exitModes.getByRole("button", { name: "Estimated" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(baselineModes.getByRole("button", { name: "Weighted" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expectPricedInMatchesApi(page, api);
  };

  await changeEverything();
  await r.getByRole("button", { name: "Reset inputs" }).click();
  await expectDefaultsFor("NVDA", "15.0", "3");
  expect((await savedLevers(page)).ticker).toBe("NVDA");

  await changeEverything();
  await pickStock(page, "KO", "KO");
  await expectDefaultsFor("KO", "12.0", "5");
});
