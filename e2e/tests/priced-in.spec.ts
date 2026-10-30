import { test } from "./support/fixtures";
import { expectPricedInMatchesApi, pickStock } from "./support/stock";

for (const [query, ticker] of [
  ["NV", "NVDA"],
  ["KO", "KO"],
] as const) {
  test(`the priced-in numbers of ${ticker} equal the API evaluation`, async ({ page, api }) => {
    await page.goto("/");
    await pickStock(page, query, ticker);
    await expectPricedInMatchesApi(page, api);
  });
}
