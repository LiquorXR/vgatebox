import { expect, test } from "@playwright/test";
import { mockApi } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await mockApi(page);
});

test("root loads the new console and exits page", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "总览" })).toBeVisible();
  await expect(page.getByText("就绪出口").first()).toBeVisible();
  await expect(page.getByText("2 / 4").first()).toBeVisible();

  await page.getByRole("link", { name: "出口" }).click();
  await expect(page.getByRole("heading", { name: "出口" })).toBeVisible();
  await expect(page.getByText("日本 · 01")).toBeVisible();
  await expect(page.getByText("韩国 · 02")).toBeVisible();
});

test("target dialog saves and closes", async ({ page }) => {
  await page.goto("/#/exits");
  await page.getByRole("button", { name: "调整数量" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.locator('input[name="target-count"]').fill("5");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("目标数量已保存，正在按策略连接")).toBeVisible();
});

test("subscription format and QR update", async ({ page }) => {
  await page.goto("/#/subscription");
  await expect(page.getByRole("heading", { name: "订阅", exact: true })).toBeVisible();
  const url = page.locator('textarea[name="subscription-url"]');
  await expect(url).toHaveValue(/token=abcdef1234567890/);
  await page.selectOption('select[name="sub-format"]', "clash");
  await expect(url).toHaveValue(/target=clash/);
  await expect(page.getByRole("img", { name: "订阅二维码" })).toBeVisible();
});

test("node search filters the candidate table", async ({ page }) => {
  await page.goto("/#/nodes");
  await expect(page.getByRole("heading", { name: "节点池" })).toBeVisible();
  await expect(page.getByText("public-vpn-101")).toBeVisible();
  await page.fill('input[name="node-search"]', "101");
  await expect(page.getByText("public-vpn-101")).toBeVisible();
  await expect(page.getByText("public-vpn-102")).toHaveCount(0);
});

test("access tabs render SSTP and third-party resources", async ({ page }) => {
  await page.goto("/#/access");
  await page.getByRole("tab", { name: /第三方 SOCKS5/ }).click();
  await expect(page.getByText("美国 · 自有代理")).toBeVisible();
  await page.getByRole("tab", { name: /原始 SSTP/ }).click();
  await expect(page.getByText("public-vpn-102")).toBeVisible();
});

test("theme choice persists across reloads", async ({ page }) => {
  await page.goto("/#/settings");
  await expect(page.getByText("v0.9.2").first()).toBeVisible();
  await page.getByRole("button", { name: "切换外观" }).click();
  await page.getByRole("menuitem", { name: "深色" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
});

test("mobile layout has the bottom navigation and no page overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "移动端主导航" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "总览" })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});

test("login shows the server error", async ({ page }) => {
  await page.route("**/login", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({ error: "口令不对" }),
        })
      : route.fallback(),
  );
  await page.goto("/login.html");
  await page.fill('input[name="password"]', "wrong");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByRole("alert")).toContainText("口令不对");
});
