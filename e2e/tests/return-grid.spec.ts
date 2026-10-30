import { expect, isPhone, test } from "./support/fixtures";
import { pct, signedPct } from "./support/format";
import { evaluateAsPage, pickStock, pricedInPanel } from "./support/stock";

test("the YOU cell sits in the baseline's row and shows the summary's yearly return", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await pickStock(page, "GR", "GRMN");
  const grid = page.locator('[data-panel="return-grid"]');
  const you = grid.locator("td.sel");
  await expect(you).toHaveCount(1);

  const ev = await evaluateAsPage(page, api);
  const at = ev.atBaseline.value;
  if (at === null) {
    throw new Error("GRMN has a return at its baseline");
  }
  // The row of the YOU cell is the exact baseline: the weighted trend.
  const row = you.locator("xpath=ancestor::tr[1]");
  await expect(row.locator("th")).toContainText(pct(ev.baselines.weighted.value));
  await expect(row.locator("th .tags")).toContainText("Wtd");
  await expect(you).toHaveText(signedPct(at.yearlyReturn, 0));

  const summary = pricedInPanel(page)
    .locator("dl.kv dt", { hasText: "Annual return at baseline" })
    .locator("xpath=following-sibling::dd[1]");
  // The summary shows the same return as the YOU cell, at one decimal.
  await expect(summary).toHaveText(signedPct(at.yearlyReturn, 1));
});

test("hovering a row tag outlines its row", async ({ page }) => {
  await page.goto("/");
  await pickStock(page, "GR", "GRMN");
  const grid = page.locator('[data-panel="return-grid"]');
  const tag = grid.locator("th .tags span", { hasText: /^Req$/ });
  const outline = grid.locator('[data-row-tag="required"]');
  await expect(outline).not.toHaveClass(/\bon\b/);
  // A finger has no hover: a tap focuses the tag, which outlines the row too.
  if (isPhone(page)) {
    await tag.tap();
  } else {
    await tag.hover();
  }
  await expect(outline).toHaveClass(/\bon\b/);
  await expect(page.getByRole("tooltip")).toBeVisible();
  // The outline wraps the row of the tag.
  const [box, rowBox] = await Promise.all([
    outline.boundingBox(),
    tag.locator("xpath=ancestor::tr[1]").boundingBox(),
  ]);
  expect(box).not.toBeNull();
  expect(rowBox).not.toBeNull();
  if (box && rowBox) {
    expect(Math.abs(box.y + box.height / 2 - (rowBox.y + rowBox.height / 2))).toBeLessThan(4);
  }
});
