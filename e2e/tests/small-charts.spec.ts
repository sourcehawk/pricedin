import type { Page } from "@playwright/test";
import { expect, isPhone, test } from "./support/fixtures";
import { pickStock } from "./support/stock";

// Each small chart, and what opens it on the page.
const charts: { name: string; panel: string; open?: (page: Page) => Promise<void> }[] = [
  { name: "earnings trajectory", panel: "earnings-trajectory" },
  { name: "how demanding", panel: "how-demanding" },
  { name: "financials bar", panel: "financials" },
  {
    name: "financials compare",
    panel: "financials",
    open: async (page) => {
      await page
        .locator('[data-panel="financials"]')
        .getByRole("button", { name: "Compare", exact: true })
        .click();
    },
  },
];

for (const c of charts) {
  test(`the ${c.name} chart snaps its tooltip and guide line to the nearest column`, async ({
    page,
  }) => {
    await page.goto("/");
    await pickStock(page, "NV", "NVDA");
    await c.open?.(page);
    const chart = page
      .locator(`[data-panel="${c.panel}"]`)
      .locator('svg[role="img"], svg[role="group"]')
      .filter({ has: page.locator("rect.vcol") })
      .first();
    const columns = chart.locator("rect.vcol");
    const guide = chart.locator("line.vx");
    await expect(guide).toHaveAttribute("visibility", "hidden");

    const count = await columns.count();
    expect(count).toBeGreaterThan(2);
    const column = columns.nth(Math.floor(count / 2));
    const box = await column.boundingBox();
    if (box === null) {
      throw new Error("the column has a box");
    }
    // Off the column's center: the guide still snaps to the center.
    const at = { x: box.width * 0.8, y: box.height / 3 };
    if (isPhone(page)) {
      await column.tap({ position: at });
    } else {
      await column.hover({ position: at });
    }
    await expect(page.getByRole("tooltip")).toBeVisible();
    await expect(guide).toHaveAttribute("visibility", "visible");
    await expect(guide).toHaveAttribute("x1", (await column.getAttribute("data-x")) ?? "");
  });
}
