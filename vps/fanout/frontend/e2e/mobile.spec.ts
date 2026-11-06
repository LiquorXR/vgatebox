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
