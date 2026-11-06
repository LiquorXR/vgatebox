import type { Page } from "@playwright/test";

// 与 playwright.config.ts 同源：E2E_BASE_PATH 含随机访问路径前缀。
export const BASE =
  (process.env.E2E_BASE || "http://127.0.0.1:18080") + (process.env.E2E_BASE_PATH || "");
const PASSWORD = process.env.E2E_PASSWORD || "";

// 真登录：未登录访问就是登录页（Auth.Wrap 直接 serve），填口令提交后进总览。
export async function login(page: Page): Promise<void> {
  await page.goto(BASE + "/");
  await page.getByRole("heading", { name: "登录控制台" }).waitFor();
  await page.locator('input[name="password"]').fill(PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByText("来源、出口、订阅，一条链路看清。").waitFor();
}
