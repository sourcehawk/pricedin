import { expect, test } from "@playwright/test";

const apiURL = process.env.PRICEDIN_E2E_API_URL ?? "http://localhost:8080";

test("the home page shows the brand", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveTitle("PricedIn");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("PricedIn");
});

test("the API reports that it is alive", async ({ request }) => {
  const response = await request.get(`${apiURL}/healthz`);
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ status: "ok" });
});

test("the API reports that it is ready", async ({ request }) => {
  const response = await request.get(`${apiURL}/readyz`);
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ status: "ok" });
});
