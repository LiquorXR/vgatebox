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
  await expect(page.getByText("vv0.9.2")).toHaveCount(0);
  await expect(page.getByText("v0.9.2").first()).toBeVisible();
  await expect(page.getByRole("button", { name: "刷新来源" })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "复制订阅" })).toHaveCount(1);

  await page.locator('nav[aria-label="主导航"] a[href="#/exits"]').click();
  await expect(page.getByRole("heading", { name: "出口" })).toBeVisible();
  await expect(page.getByText("日本 · 01")).toBeVisible();
  await expect(page.getByText("韩国 · 02")).toBeVisible();
});

test("exit drawer keeps policy edits and offers a node swap", async ({ page }) => {
  await page.goto("/#/exits?slot=1");
  await expect(page.getByRole("dialog").getByText("日本 · 01", { exact: false })).toBeVisible();
  await expect(page.getByText("SOCKS5 凭据")).toHaveCount(0);

  const country = page.locator('select[name="policy-country"]');
  await expect(country.locator("option").filter({ hasText: "日本" })).toHaveCount(1);

  const checkbox = page.locator('input[name="policy-ip-type"]').first();
  const before = await checkbox.isChecked();
  await checkbox.click();
  await page.waitForTimeout(5500);
  expect(await checkbox.isChecked()).toBe(!before);

  await page.getByRole("button", { name: "更换 IP" }).click();
  await expect(page.getByText("将断开当前节点并重新挑选一个候选", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "更换 IP" }).last().click();
  await expect(page.getByText("正在更换节点；出口编号和端口保持不变")).toBeVisible();
});

test("target dialog shows region inventory and saves", async ({ page }) => {
  await page.goto("/#/exits");
  await page.getByRole("button", { name: "调整数量" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  const japan = page.locator('select[name="target-country"] option').filter({ hasText: "日本（JP）" });
  await expect(japan).toContainText("12 个空闲");
  await expect(japan).toContainText("35ms");
  await expect(japan).toContainText("48.5Mbps");
  await page.locator('input[name="target-count"]').fill("5");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("目标数量已保存，正在按策略连接")).toBeVisible();
});

test("exit deletion reduces the target count", async ({ page }) => {
  await page.goto("/#/exits?slot=1");
  await expect(page.getByRole("dialog").getByText("日本 · 01", { exact: false })).toBeVisible();
  const request = page.waitForRequest(
    (req) => req.url().includes("/api/console/slot/delete") && req.method() === "POST",
  );
  await page.getByRole("dialog").getByRole("button", { name: "删除出口" }).click();
  const confirm = page.getByRole("dialog").filter({ hasText: "删除后目标数量会减少 1" });
  await expect(confirm).toBeVisible();
  await expect(confirm.getByText("同时清理归档入口")).toBeVisible();
  await confirm.getByRole("button", { name: "删除出口" }).click();
  expect((await request).postDataJSON()).toEqual({ slot: 1, cleanup_archive: false });
  await expect(page.getByText("出口已删除，目标数量已减少 1")).toBeVisible();
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
  await page.getByRole("button", { name: "详情" }).first().click();
  await expect(page.getByText("外部连接地址")).toBeVisible();
  await expect(page.getByText("服务器本机地址")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "关闭", exact: true }).click();

  await page.getByRole("tab", { name: /第三方 SOCKS5/ }).click();
  await expect(page.getByText("美国 · 自有代理")).toBeVisible();
  await page.getByRole("tab", { name: /原始 SSTP/ }).click();
  await expect(page.getByText("public-vpn-102")).toBeVisible();
  await page.getByRole("tab", { name: /高级接入/ }).click();
  await expect(page.getByText("自动入口 · in-1")).toBeVisible();
  await expect(page.getByText("本机直出 · host-direct")).toBeVisible();

  await page.getByRole("button", { name: "新增接入入口" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.locator('select').filter({ hasText: "VLESS" }).first()).toBeVisible();
  await expect(page.getByRole("dialog").locator("select").nth(2)).toHaveValue("xhttp");
  await expect(page.getByRole("dialog").locator("select").nth(3)).toHaveValue("reality");
  await page.getByRole("button", { name: "取消" }).click();

  await page
    .getByRole("row")
    .filter({ hasText: "host-direct" })
    .getByRole("button", { name: "删除", exact: true })
    .click();
  await expect(page.getByText("删除接入入口")).toBeVisible();
  await page.getByRole("button", { name: "取消" }).click();

  await page.getByRole("button", { name: "详情" }).first().click();
  await expect(page.getByText("客户端 · 1")).toBeVisible();
  await expect(page.getByText("client@example")).toBeVisible();

  const cloneRequest = page.waitForRequest(
    (req) => req.url().includes("/api/xui/clone") && req.url().includes("id=101"),
  );
  await page.getByRole("button", { name: "复制到所有就绪出口" }).click();
  await expect(page.getByText("已有入口不会被删除", { exact: false })).toBeVisible();
  await page.getByRole("dialog").filter({ hasText: "已有入口不会被删除" }).getByRole("button", { name: "复制" }).click();
  await cloneRequest;
  await expect(page.getByText("已创建 2 个接入入口")).toBeVisible();
});

test("managed exit deletion uses the console target API", async ({ page }) => {
  await page.goto("/#/access");
  await page.getByRole("tab", { name: /高级接入/ }).click();
  const request = page.waitForRequest(
    (req) => req.url().includes("/api/console/slot/delete") && req.method() === "POST",
  );
  await page.getByRole("row").filter({ hasText: "自动入口 · in-1" }).getByRole("button", { name: "删除出口" }).click();
  const confirm = page.getByRole("dialog").filter({ hasText: "目标数量会自动减少 1" });
  await expect(confirm).toBeVisible();
  await confirm.locator('input[name="cleanup-archive"]').check();
  await confirm.getByRole("button", { name: "删除出口" }).click();
  expect((await request).postDataJSON()).toEqual({ slot: 1, cleanup_archive: true });
  await expect(page.getByText("自动出口和归档入口已删除")).toBeVisible();
});

test("archived automatic entry can be cleaned without an exit", async ({ page }) => {
  await page.goto("/#/access");
  await page.getByRole("tab", { name: /高级接入/ }).click();
  const row = page.getByRole("row").filter({ hasText: "in-30006-tcp" });
  await expect(row.getByText("归档入口", { exact: false })).toBeVisible();
  const request = page.waitForRequest(
    (req) => req.url().includes("/api/xui/delete") && req.url().includes("ids=906"),
  );
  await row.getByRole("button", { name: "清理入口" }).click();
  const confirm = page.getByRole("dialog").filter({ hasText: "对应出口已不存在" });
  await expect(confirm).toBeVisible();
  await confirm.getByRole("button", { name: "清理" }).click();
  await request;
  await expect(page.getByText("归档入口已清理")).toBeVisible();
});

test("socks credentials can be edited from the access drawer", async ({ page }) => {
  await page.goto("/#/access");
  await page.getByRole("button", { name: "详情" }).first().click();
  await expect(page.getByText("SOCKS5 凭据")).toBeVisible();
  await page.locator('input[name="socks-user"]').fill("rotated-user");
  await page.locator('input[name="socks-pass"]').fill("rotated-pass");
  const request = page.waitForRequest(
    (req) => req.url().includes("/api/cred") && req.method() === "POST",
  );
  await page.getByRole("button", { name: "保存凭据" }).click();
  const url = new URL((await request).url());
  expect(url.searchParams.get("slot")).toBe("1");
  expect(url.searchParams.get("user")).toBe("rotated-user");
  expect(url.searchParams.get("pass")).toBe("rotated-pass");
  await expect(page.getByText("SOCKS5 凭据已保存")).toBeVisible();
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
