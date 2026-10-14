import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  use: { baseURL: "http://127.0.0.1:18765", headless: true },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: { command: "node e2e/serve.mjs", url: "http://127.0.0.1:18765/api/v1/health", reuseExistingServer: false, timeout: 30_000 },
});
