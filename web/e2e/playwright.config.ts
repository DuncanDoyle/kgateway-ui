import { defineConfig } from "@playwright/test";

// Runs against the UI that test/e2e/setup.sh port-forwards; it starts no server.
export default defineConfig({
  testDir: ".",
  outputDir: "../test-results",
  reporter: [["list"], ["html", { outputFolder: "../playwright-report", open: "never" }]],
  use: {
    baseURL: process.env.UI_URL ?? "http://127.0.0.1:18080",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  timeout: 120_000,
});
