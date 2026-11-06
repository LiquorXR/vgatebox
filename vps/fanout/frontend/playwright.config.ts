import { defineConfig, devices } from "@playwright/test";

// 真后端由 CI 起（sudo + 临时 workdir，见 .github/workflows/e2e.yml），
// 这里只拼地址：E2E_BASE 含随机访问路径前缀（如 http://127.0.0.1:18080/aB3xY9pQ）。
const baseURL = (process.env.E2E_BASE || "http://127.0.0.1:18080") + (process.env.E2E_BASE_PATH || "");

export default defineConfig({
  testDir: "./e2e",
  // 后端状态共享（目标数量/直出开关），全文件串行，文件内默认串行。
  fullyParallel: false,
  workers: 1,
  timeout: 60000,
  expect: { timeout: 15000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : [["list"]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "desktop",
      testMatch: /flows\.spec\.ts/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1280, height: 720 },
        permissions: ["clipboard-read", "clipboard-write"],
      },
    },
    {
      name: "mobile",
      testMatch: /mobile\.spec\.ts/,
      use: { ...devices["Pixel 5"] },
    },
  ],
});
