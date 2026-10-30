import { expect, test } from "./support/fixtures";
import { fixture } from "./support/stack";
import { expectPricedInMatchesApi, pricedInPanel } from "./support/stock";

test("a company that is known but not loaded can be loaded from the search and then used", async ({
  page,
  api,
}, testInfo) => {
  // The load may take up to a minute, as the page says.
  test.setTimeout(90_000);
  // Loading changes the stack for every later test, so one project runs it.
  test.skip(testInfo.project.name !== "desktop-light", "loads the company once per run");
  const ticker = fixture.unloaded;
  const before = await api.profile(ticker);
  // A retry can find the company loaded by the first try. It then checks
  // only that the loaded company is usable, so a failure there still fails.
  const loadedEarlier = before.loaded && testInfo.retry > 0;
  if (!loadedEarlier) {
    expect(before.loaded, `the seed leaves ${ticker} unloaded`).toBe(false);
  }

  await page.goto("/");
  const box = page.getByRole("combobox", { name: "Pick a stock" });
  await box.click();
  await box.pressSequentially(ticker);
  const option = page.getByRole("option").filter({ hasText: ticker });
  await expect(option).toBeVisible();
  if (!loadedEarlier) {
    await expect(option).toContainText("Load this company");
  }
  await box.press("Enter");

  if (!loadedEarlier) {
    await expect(page.getByRole("status")).toContainText(`Loading ${ticker}`);
  }
  await expect(pricedInPanel(page)).toBeVisible({ timeout: 60_000 });
  await expect(page.locator("header.top .co b")).toHaveText(before.company.name);
  expect((await api.profile(ticker)).loaded).toBe(true);
  await expectPricedInMatchesApi(page, api);
});
