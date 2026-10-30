import type { Page } from "@playwright/test";
import { expect, test } from "./support/fixtures";
import { apiURL, newClientIP, owner } from "./support/stack";
import { pickStock, pricedInPanel } from "./support/stock";

test.use({ storageState: { cookies: [], origins: [] } });

// Each test logs in from an address of its own, so that its attempts never
// meet the login rate limit of another test.
test.beforeEach(async ({ context }) => {
  await context.setExtraHTTPHeaders({ "x-real-ip": newClientIP() });
});

async function logIn(page: Page, password: string) {
  await page.getByLabel("Email").fill(owner.email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Log in" }).click();
}

test("a wrong password shows an error and keeps the visitor on the login page", async ({
  page,
}) => {
  await page.goto("/");
  await logIn(page, `${owner.password}-wrong`);
  await expect(page.locator(".login").getByRole("alert")).toHaveText(
    "The email or the password is wrong.",
  );
  await expect(page).toHaveURL(/\/login/);
});

test("too many attempts show the rate limit with the wait", async ({ page }) => {
  await page.goto("/login");
  const alert = page.locator(".login").getByRole("alert");
  // The server allows 5 attempts a minute for one address.
  for (let i = 0; i < 5; i++) {
    await logIn(page, `${owner.password}-wrong-${String(i)}`);
    await expect(alert).toHaveText("The email or the password is wrong.");
  }
  await logIn(page, owner.password);
  await expect(alert).toHaveText(/^Too many attempts\. Try again in \d+ seconds\.$/);
  await expect(page).toHaveURL(/\/login/);
});

test("the right password opens the page that sent the visitor to log in", async ({ page }) => {
  await page.goto("/data");
  await expect(page).toHaveURL(/\/login\?next=%2Fdata$/);
  await logIn(page, owner.password);
  await expect(page).toHaveURL(/\/data$/);
  await expect(page.getByRole("heading", { name: "Data freshness" })).toBeVisible();
  await page.getByRole("link", { name: "Analysis" }).click();
  await expect(page.getByRole("heading", { name: "Pick a stock" })).toBeVisible();
});

test("an API call without a session is refused", async ({ playwright }) => {
  const anonymous = await playwright.request.newContext();
  for (const url of [`${apiURL}/api/v1/stocks/NVDA`, "/api/v1/stocks/NVDA"]) {
    const r = await anonymous.get(
      url.startsWith("/") ? new URL(url, test.info().project.use.baseURL).href : url,
    );
    expect(r.status(), url).toBe(401);
    expect(r.headers()["www-authenticate"], url).toBeTruthy();
  }
  await anonymous.dispose();
});

test("logging out ends the session and returns to the login page", async ({ page }) => {
  await page.goto("/login");
  await logIn(page, owner.password);
  await expect(page.getByRole("heading", { name: "Pick a stock" })).toBeVisible();
  expect((await page.request.get("/api/auth/session")).status()).toBe(200);

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect((await page.request.get("/api/v1/stocks/NVDA")).status()).toBe(401);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login\?next=%2F$/);
});

test("a new login opens the stock that was open before the logout", async ({ page }) => {
  await page.goto("/login");
  await logIn(page, owner.password);
  await expect(page.getByRole("heading", { name: "Pick a stock" })).toBeVisible();
  await pickStock(page, "KO", "KO");
  const company = page.locator("header.top .co");
  await expect(company).toContainText("KO");
  const name = await company.locator("b").innerText();

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await logIn(page, owner.password);
  await expect(pricedInPanel(page)).toBeVisible();
  await expect(company.locator("b")).toHaveText(name);
  await expect(company.locator("span")).toHaveText(/(^| · )KO$/);
});

test("the page follows the color scheme of the system", async ({ page }) => {
  await page.goto("/login");
  const dark = await page.evaluate(() => matchMedia("(prefers-color-scheme: dark)").matches);
  expect(dark).toBe(test.info().project.use.colorScheme === "dark");
  const lightness = await page.evaluate(() => {
    const [r = 0, g = 0, b = 0] = (
      getComputedStyle(document.body).backgroundColor.match(/\d+/g) ?? []
    ).map(Number);
    return (r + g + b) / 3;
  });
  if (dark) {
    expect(lightness).toBeLessThan(80);
  } else {
    expect(lightness).toBeGreaterThan(180);
  }
});
