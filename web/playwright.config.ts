import { defineConfig } from "@playwright/test";

const baseURL = process.env.SUPERBOT_E2E_BASE_URL || "http://127.0.0.1:3000";

export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL,
    channel: "chrome",
    headless: true,
    // The app registers a shell service worker; during deploys it can serve a
    // stale/aborted navigation and show Chrome's "This page couldn't load".
    // Functional tests do not exercise push/offline, so bypass it.
    serviceWorkers: "block",
    viewport: { width: 1280, height: 860 },
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
});
