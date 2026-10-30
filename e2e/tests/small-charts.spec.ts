import { expect, isPhone, test } from "./support/fixtures";
import { pickStock } from "./support/stock";

for (const panel of ["earnings-trajectory", "how-demanding"]) {
  test(`the ${panel} chart snaps its tooltip and guide line to the nearest column`, async ({
    page,
  }) => {
    await page.goto("/");
    await pickStock(page, "NV", "NVDA");
    const chart = page.locator(`[data-panel="${panel}"] svg[role="img"]`).first();
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
