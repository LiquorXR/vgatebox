import { expect, test } from "@playwright/test";
import { BASE, login } from "./helpers";

// 只跑在 mobile 项目（Pixel 5）：底部导航 + 接入页签，只读不断言业务状态。
test("mobile nav and access tabs", async ({ page }) => {
  await login(page);
  await page.goto(BASE + "/#/access");
  const nav = page.getByRole("navigation", { name: "移动端主导航" });
  await expect(nav).toBeVisible();
  await nav.getByRole("link", { name: "订阅" }).click();
  await expect(page).toHaveURL(/#\/subscription/);
  await expect(page.getByText("选择客户端格式和订阅范围")).toBeVisible();
  await page.goto(BASE + "/#/access");
  const tab = page.getByRole("tab", { name: /第三方 SOCKS5/ });
  await tab.click();
  await expect(tab).toHaveAttribute("aria-selected", "true");
});

test("mobile exit drawer opens and closes", async ({ page }) => {
  await login(page);
  await page.goto(BASE + "/#/exits");
  // E2E 后端可能是零隧道：没有可点的行就跳过，不断言。
  const rows = page.getByRole("button", { name: /查看详情/ });
  if ((await rows.count()) === 0) test.skip(true, "no exits to open");
  // 列表行整个可点：打开第一条出口的抽屉。
  await rows.first().click();
  const drawer = page.getByRole("dialog");
  await expect(drawer).toBeVisible();
  await drawer.getByRole("button", { name: "关闭" }).click();
  await expect(drawer).toBeHidden();
});
