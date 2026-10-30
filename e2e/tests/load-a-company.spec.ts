import { expect, test } from "./support/fixtures";
import { fixture } from "./support/stack";
import { evaluateAsPage, expectPricedInMatchesApi, pricedInPanel } from "./support/stock";

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

  // The loaded filings drive the price chart: its share-count markers are the API's events.
  // The 3Y range reaches the company's buybacks of 2024.
  const chartPanel = page.locator('[data-panel="price-chart"]');
  const range = chartPanel
    .getByRole("group", { name: "Chart range" })
    .getByRole("button", { name: "3Y", exact: true });
  await range.click();
  await expect(range).toHaveAttribute("aria-pressed", "true");
  const chart = (await evaluateAsPage(page, api)).priceChart.value;
  expect(chart?.range).toBe("3Y");
  const events = chart?.shareCountEvents ?? [];
  expect(events.length, `${ticker} has share-count events in its 3Y chart`).toBeGreaterThan(0);
  await expect(chartPanel.locator(".pxev")).toHaveCount(events.length);
  const tones = await page.evaluate(() => {
    const probe = document.createElement("i");
    document.body.append(probe);
    const tone = (token: string) => {
      probe.style.color = `var(${token})`;
      return getComputedStyle(probe).color;
    };
    const colors = { good: tone("--good"), bad: tone("--bad") };
    probe.remove();
    return colors;
  });
  for (const e of events) {
    const marker = chartPanel.locator(`.pxev[aria-label^="${e.label}: ${e.kind},"]`);
    await expect(marker).toHaveClass(new RegExp(`\\b${e.kind}\\b`));
    const stroke = await marker.locator("rect").evaluate((r) => getComputedStyle(r).stroke);
    expect(stroke, `the tone of ${e.label}`).toBe(e.kind === "dilution" ? tones.bad : tones.good);
  }
});
