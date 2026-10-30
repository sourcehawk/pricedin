import { expect, test } from "./support/fixtures";
import { usd } from "./support/format";
import { pickStock } from "./support/stock";

test("picking a stock from the search fills the top bar and the starting point from its profile", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Pick a stock" })).toBeVisible();
  // By name, not ticker: "coca" finds Coca-Cola.
  await pickStock(page, "coca", "KO");
  const p = await api.profile("KO");
  const close = p.latestClose;
  const eps = p.ttmEps;
  if (close === null || eps === null) {
    throw new Error("the KO fixture has a close and a TTM EPS");
  }

  const top = page.locator("header.top");
  await expect(top.locator(".co b")).toHaveText(p.company.name);
  const stat = (label: string) => top.locator(".stat", { hasText: label }).locator("b");
  await expect(stat("Price")).toHaveText(usd(close.value));
  await expect(stat("TTM EPS")).toHaveText(usd(eps.value));
  await expect(stat("As of close")).toHaveText(close.asOf);
  expect(close.asOfLabel).toBe("as of close");

  const rail = page.getByRole("complementary", { name: "Assumptions" });
  await expect(rail.getByLabel("Share price ($)")).toHaveValue(String(close.value));
  await expect(rail.getByLabel("TTM EPS ($)")).toHaveValue(String(eps.value));
  await expect(rail.getByText(`Filled from KO: the close of ${close.asOf}`)).toBeVisible();

  // A second pick with the arrow keys: "a" lists more than one company.
  await pickStock(page, "a", "AAPL");
  const apple = await api.profile("AAPL");
  await expect(top.locator(".co b")).toHaveText(apple.company.name);
  await expect(stat("Price")).toHaveText(usd(apple.latestClose?.value));
});
