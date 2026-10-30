import { defineConfig, devices } from "@playwright/test";
import { ownerState } from "./tests/support/stack";

const baseURL = process.env.PRICEDIN_E2E_BASE_URL ?? "http://localhost:3000";

const desktop = { ...devices["Desktop Chrome"], viewport: { width: 1600, height: 1000 } };
const phone = {
  ...devices["Desktop Chrome"],
  viewport: { width: 390, height: 844 },
  deviceScaleFactor: 3,
  isMobile: true,
  hasTouch: true,
};

// The tests run against a running stack (make test-e2e starts it with
// Docker Compose), so this config starts no web server.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  // A GitHub runner has 4 cores; the default would use 2 of them.
  ...(process.env.CI ? { workers: 4 } : {}),
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report" }]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "setup", testMatch: /\.setup\.ts$/ },
    ...(
      [
        ["desktop-light", desktop, "light"],
        ["desktop-dark", desktop, "dark"],
        ["phone-light", phone, "light"],
        ["phone-dark", phone, "dark"],
      ] as const
    ).map(([name, device, colorScheme]) => ({
      name,
      dependencies: ["setup"],
      use: { ...device, colorScheme, storageState: ownerState },
    })),
  ],
});
