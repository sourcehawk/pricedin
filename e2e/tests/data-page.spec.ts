import { expect, test } from "./support/fixtures";

test("the data page shows the seed's ingest runs and the latest price date", async ({
  page,
  api,
}) => {
  await page.goto("/data");
  const status = await api.status();
  expect(status.latestPriceDate).not.toBeNull();
  const fresh = page.getByRole("region", { name: "Data freshness" });
  await expect(fresh.locator(".tile", { hasText: "Prices" }).locator("b")).toHaveText(
    status.latestPriceDate ?? "",
  );
  expect((await api.profile("NVDA")).latestClose?.asOf).toBe(status.latestPriceDate);

  const runs = page.getByRole("region", { name: "Latest ingest runs" });
  for (const job of [
    "sec-tickers",
    "sec-bulk",
    "price-reference",
    "prices-backfill",
    "fundamentals-rebuild",
  ]) {
    const run = status.ingestRuns.find((r) => r.job === job);
    expect(run?.status, job).toBe("succeeded");
    const row = runs.locator("tbody tr", {
      has: page.getByRole("rowheader", { name: job, exact: true }),
    });
    await expect(row.locator(".pill")).toHaveText("succeeded");
  }
});
