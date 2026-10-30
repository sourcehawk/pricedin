import type { Locator, Page } from "@playwright/test";
import { expect, isPhone, test, type Api, type Chart } from "./support/fixtures";
import { day, signedPct, times, usd } from "./support/format";
import { fixture } from "./support/stack";
import { evaluateAsPage, pickStock, pricedInPanel, savedLevers } from "./support/stock";

/** literal escapes text for a regular expression. */
const literal = (text: string): string => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const chartPanel = (page: Page): Locator => page.locator('[data-panel="price-chart"]');
const chips = (page: Page): Locator =>
  chartPanel(page).getByRole("group", { name: "Lines on the price chart" });
/** chip finds a chip by its line's name; the value of the day can follow the name. */
const chip = (page: Page, name: string): Locator =>
  chips(page).getByRole("button", { name: new RegExp(`^${literal(name)}(?!\\s)`) });

/** The switchable lines of the chart, by chip name, with the layer that each one draws. */
const layers = [
  { chip: "TTM EPS", layer: ".L-eps" },
  { chip: "P/E", layer: ".L-pe" },
  { chip: "Price change, 12M", layer: ".L-r12" },
  { chip: "EPS growth, 12M", layer: ".L-g12" },
  { chip: "P/E bands", layer: "[data-bm]" },
  { chip: "Share changes", layer: ".pxev" },
] as const;

/** chartAsPage returns the API's price chart for the page's stock and levers. */
async function chartAsPage(page: Page, api: Api): Promise<Chart> {
  const chart = (await evaluateAsPage(page, api)).priceChart;
  if (chart.value === null) {
    throw new Error(`the stock has no price chart: ${chart.reason}`);
  }
  return chart.value;
}

/** pickRange presses a range button and waits until the page saves that range. */
async function pickRange(page: Page, range: "6M" | "1Y" | "3Y"): Promise<void> {
  const button = chartPanel(page)
    .getByRole("group", { name: "Chart range" })
    .getByRole("button", { name: range, exact: true });
  await button.click();
  await expect(button).toHaveAttribute("aria-pressed", "true");
  await expect.poll(async () => (await savedLevers(page)).chartRange).toBe(range);
}

/** press clicks the element, or taps it on a phone. */
async function press(page: Page, target: Locator): Promise<void> {
  if (isPhone(page)) {
    await target.tap();
  } else {
    await target.click();
  }
}

/** point moves the pointer to a point of the element, or taps it on a phone. */
async function point(page: Page, target: Locator, at: { x: number; y: number }): Promise<void> {
  if (isPhone(page)) {
    await target.tap({ position: at });
  } else {
    await target.hover({ position: at });
  }
}

test("each chart range shows the span and the price change that the API computes", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const panel = chartPanel(page);
  const sub = panel.getByTestId("price-sub");
  const starts = new Set<string>();
  for (const range of ["6M", "1Y", "3Y"] as const) {
    await pickRange(page, range);
    const chart = await chartAsPage(page, api);
    expect(chart.range).toBe(range);
    await expect(sub).toContainText(
      `${range}: ${signedPct(chart.priceChange, 1)} · high ${usd(chart.high)} · low ${usd(chart.low)} · P/E ${times(chart.peStart.value)} → ${times(chart.peEnd.value)}`,
    );
    // The plot's own description names the first and the last close it draws.
    const first = chart.points[0];
    const last = chart.points.at(-1);
    await expect(panel.getByRole("img", { name: /daily close/ })).toHaveAccessibleName(
      new RegExp(`from ${literal(usd(first?.close))} to ${literal(usd(last?.close))}`),
    );
    starts.add(chart.start);
  }
  // Three ranges, three spans: each range changes the days that the chart shows.
  expect(starts.size).toBe(3);
});

test("each overlay chip switches its line on and off", async ({ page }) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const svg = chartPanel(page).locator("svg[role=group]");
  for (const { chip: name, layer } of layers) {
    const button = chip(page, name);
    const was = (await button.getAttribute("aria-pressed")) === "true";
    for (const on of [!was, was]) {
      await press(page, button);
      await expect(button).toHaveAttribute("aria-pressed", String(on));
      // No seeded stock has a share-count change of 2% in the chart, so its markers have nothing to draw.
      if (name !== "Share changes") {
        if (on) {
          await expect(svg.locator(layer)).not.toHaveCount(0);
        } else {
          await expect(svg.locator(layer)).toHaveCount(0);
        }
      }
    }
  }
});

test("hovering a day snaps the guide line to it and shows its values from the API", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  await pickRange(page, "6M");
  const panel = chartPanel(page);
  const svg = panel.locator("svg[role=group]");
  const guide = panel.getByTestId("price-guide");
  const tip = page.getByRole("tooltip");
  const chart = await chartAsPage(page, api);
  const history = await api.history("NVDA", "6M");
  await expect(guide).toHaveAttribute("visibility", "hidden");

  const box = await svg.boundingBox();
  if (box === null) {
    throw new Error("the chart has a box");
  }
  // On a phone a tap on the tooltip drops the day. The tooltip sits beside the
  // tapped point, so both taps below use one height.
  const y = box.height / 2;
  // Between two days: the guide snaps to one of them.
  await point(page, svg, { x: box.width * 0.4 + 0.3, y });
  await expect(guide).toHaveAttribute("visibility", "visible");
  await expect(tip).toBeVisible();
  const label = await tip.locator("b").first().innerText();
  const p = chart.points.find((q) => day(q.date) === label);
  expect(p, `the tooltip's day ${label} is a day of the chart`).toBeDefined();
  const date = p?.date ?? "";
  const close = history.history.daily.find((d) => d.date === date)?.close;
  expect(close, `the history has a close on ${date}`).toBeDefined();
  await expect(tip.locator(".prow", { hasText: "Close" }).locator("strong")).toHaveText(usd(close));

  // The chips follow the hovered day.
  await expect(
    chips(page).getByText("Price", { exact: true }).locator("xpath=..").locator("b"),
  ).toHaveText(usd(close));
  await expect(chip(page, "TTM EPS").locator("b")).toHaveText(usd(p?.ttmEps));
  await expect(chip(page, "P/E").locator("b")).toHaveText(times(p?.pe.value));

  // The guide sits on the day: pointing at the guide itself keeps the same day.
  const x1 = Number(await guide.getAttribute("x1"));
  const viewBox = (await svg.getAttribute("viewBox"))?.split(" ").map(Number) ?? [];
  const width = viewBox[2] ?? 0;
  await point(page, svg, { x: (x1 / width) * box.width, y });
  await expect(tip.locator("b").first()).toHaveText(label);
  await expect(guide).toHaveAttribute("x1", String(x1));
});

test("hovering a P/E band names it in the caption above the chart, not in a tooltip", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const panel = chartPanel(page);
  const bandsChip = chip(page, "P/E bands");
  if ((await bandsChip.getAttribute("aria-pressed")) !== "true") {
    await press(page, bandsChip);
  }
  const caption = panel.getByTestId("band-caption");
  await expect(caption).not.toHaveClass(/\bon\b/);
  const bands = (await chartAsPage(page, api)).bands;
  if (bands.value === null) {
    throw new Error("NVDA has P/E bands");
  }
  // The labels at the right edge come after the band lines.
  const band = panel.getByRole("img", { name: /× band:/ }).last();
  const multiple = Number(await band.getAttribute("data-bm"));
  const want = bands.value.multiples.find((b) => b.multiple === multiple);
  expect(want, `the API has the ${String(multiple)}× band`).toBeDefined();
  const box = await band.boundingBox();
  if (box === null) {
    throw new Error("the band label has a box");
  }
  await point(page, band, { x: box.width / 2, y: box.height / 2 });
  await expect(caption).toHaveClass(/\bon\b/);
  await expect(caption).toContainText(`${String(multiple)}× band:`);
  await expect(caption).toContainText(usd(want?.priceToday.value));
  await expect(page.getByRole("tooltip")).toHaveCount(0);
});

test("a share-count marker of a dilution never takes the good tone", async ({ page, api }) => {
  const withEvents: string[] = [];
  for (const ticker of fixture.loaded) {
    const ev = await api.evaluate(ticker, {
      targetReturn: 0.15,
      holdingYears: 3,
      chartRange: "3Y",
    });
    const chart = ev.evaluation.priceChart;
    if (chart.value !== null && chart.value.shareCountEvents.length > 0) {
      withEvents.push(ticker);
    }
  }
  test.skip(
    withEvents.length === 0,
    "no seeded stock has a share-count change of 2% in its 3Y chart; PriceChartPanel.test.tsx covers the tones",
  );
  for (const ticker of withEvents) {
    await page.goto("/");
    await pickStock(page, ticker, ticker);
    await pickRange(page, "3Y");
    const events = (await chartAsPage(page, api)).shareCountEvents;
    const markers = chartPanel(page).locator(".pxev");
    await expect(markers).toHaveCount(events.length);
    const tones = await page.evaluate(() => {
      const probe = document.createElement("i");
      document.body.append(probe);
      const tone = (v: string) => {
        probe.style.color = `var(${v})`;
        return getComputedStyle(probe).color;
      };
      const out = { good: tone("--good"), bad: tone("--bad") };
      probe.remove();
      return out;
    });
    for (const e of events) {
      const marker = chartPanel(page).locator(`.pxev[aria-label^="${e.label}: "]`);
      await expect(marker).toHaveClass(new RegExp(`\\b${e.kind}\\b`));
      const stroke = await marker.locator("rect").evaluate((r) => getComputedStyle(r).stroke);
      expect(stroke).toBe(e.kind === "dilution" ? tones.bad : tones.good);
    }
  }
});

test("the walkthrough shows every line while it runs and then puts the chips back", async ({
  page,
}) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const pressed = async () =>
    Promise.all(layers.map(async (l) => chip(page, l.chip).getAttribute("aria-pressed")));
  const before = await pressed();
  expect(before, "some line is off before the walkthrough").toContain("false");

  await page
    .getByRole("button", { name: "Walk me through the price chart with my numbers" })
    .click();
  const card = page.getByRole("region", { name: "Walkthrough of the price chart" });
  await expect(card).toBeVisible();
  for (const l of layers) {
    await expect(chip(page, l.chip)).toHaveAttribute("aria-pressed", "true");
  }
  await card.getByRole("button", { name: "End the walkthrough" }).click();
  await expect(card).toHaveCount(0);
  expect(await pressed()).toEqual(before);
});

test("on a wide screen the priced-in panel and the price chart sit side by side at equal height", async ({
  page,
}) => {
  test.skip(isPhone(page), "the panels stack on a phone; they sit side by side from 1600 px");
  expect(page.viewportSize()?.width ?? 0).toBeGreaterThanOrEqual(1600);
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const a = await pricedInPanel(page).boundingBox();
  const b = await chartPanel(page).boundingBox();
  if (a === null || b === null) {
    throw new Error("both panels have a box");
  }
  expect(b.x, "the chart is right of the priced-in panel").toBeGreaterThanOrEqual(a.x + a.width);
  expect(Math.abs(a.y - b.y)).toBeLessThanOrEqual(1);
  expect(Math.abs(a.height - b.height)).toBeLessThanOrEqual(1);
});
