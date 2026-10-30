import { expect, test } from "./support/fixtures";
import { pickStock } from "./support/stock";

const walkthroughs = [
  { name: "the inputs", launcher: "Walk me through these inputs" },
  {
    name: "the priced-in summary",
    launcher: "Walk me through the priced-in summary with my numbers",
  },
  {
    name: "the price chart",
    launcher: "Walk me through the price chart with my numbers",
  },
  { name: "the return grid", launcher: "Walk me through this grid with my numbers" },
  {
    name: "the earnings trajectory",
    launcher: "Walk me through the earnings trajectory with my numbers",
  },
  {
    name: "how demanding the stock has been",
    launcher: "Walk me through how demanding the stock has been",
  },
  {
    name: "the bear, base and bull cases",
    launcher: "Walk me through the three cases with my numbers",
  },
  { name: "the financials", launcher: "Walk me through the financials with my numbers" },
];

for (const w of walkthroughs) {
  test(`the walkthrough of ${w.name} steps in place to its take-away`, async ({ page }) => {
    await page.goto("/");
    await pickStock(page, "NV", "NVDA");
    const launcher = page.getByRole("button", { name: w.launcher });
    await launcher.scrollIntoViewIfNeeded();
    const scrollY = await page.evaluate(() => window.scrollY);
    await launcher.click();

    const card = page.getByRole("region", { name: `Walkthrough of ${w.name}` });
    await expect(card).toBeVisible();
    const count = Number(
      (/of (\d+)$/.exec((await card.locator(".t-step").textContent()) ?? "") ?? [])[1],
    );
    expect(count).toBeGreaterThan(1);

    let highlighted = 0;
    let faded = 0;
    let ringed = 0;
    for (let step = 1; step <= count; step++) {
      await expect(card.locator(".t-step")).toHaveText(
        `Walkthrough · step ${String(step)} of ${String(count)}`,
      );
      await expect(page.getByRole("dialog")).toHaveCount(0);
      expect(
        await page.evaluate(() => window.scrollY),
        `the page scrolled at step ${String(step)}`,
      ).toBe(scrollY);
      // Each step adds one line with the user's own numbers.
      await expect(card.locator(".t-you").first()).not.toBeEmpty();
      highlighted += await page.locator("[data-thl]").count();
      faded += await page.locator("[data-tl]").count();
      ringed += await page.locator("[data-tside]").count();
      if (step < count) {
        // The arrow key, not a click: a click would scroll the button into view.
        await page.keyboard.press("ArrowRight");
      }
    }
    await expect(card.locator(".t-takeaway")).not.toBeEmpty();
    // The walkthrough of the inputs explains the levers, so it rings them
    // and highlights nothing in a panel.
    if (w.name !== "the inputs") {
      expect(highlighted, "some step highlights the parts it explains").toBeGreaterThan(0);
      expect(faded, "some step fades the rest").toBeGreaterThan(0);
    }
    expect(ringed, "some step rings a lever").toBeGreaterThan(0);

    await expect(card.getByRole("button", { name: "Finish" })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(card).toHaveCount(0);
    await expect(page.locator("[data-thl], [data-tl], [data-tside]")).toHaveCount(0);
    expect(await page.evaluate(() => window.scrollY)).toBe(scrollY);
  });
}
