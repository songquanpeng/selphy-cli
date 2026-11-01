import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "*.spec.js",
  fullyParallel: true,
  use: {
    baseURL: "http://127.0.0.1:4179",
    viewport: { width: 1180, height: 790 },
    headless: true,
    channel: process.env.CI ? undefined : "chrome",
  },
  webServer: {
    command: "node tests/server.mjs",
    url: "http://127.0.0.1:4179",
    reuseExistingServer: !process.env.CI,
  },
  reporter: "list",
});
