import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.PRICEDIN_E2E_BASE_URL ?? "http://localhost:3000";

// The tests run against a running stack (make test-e2e starts it with
// Docker Compose), so this config starts no web server.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report" }]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
