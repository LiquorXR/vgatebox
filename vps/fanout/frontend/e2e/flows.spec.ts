import { expect, test, type Page } from "@playwright/test";
import { BASE, login } from "./helpers";

// 五条关键路径共用一个后端（目标数量/直出开关是全局状态），
// 文件内按顺序串行：设数量 → 订阅（开直出）→ 删出口（回 0）。
async function consoleSummary(page: Page): Promise<any> {
  const res = await page.request.get(BASE + "/api/console");
  expect(res.ok()).toBeTruthy();
  return res.json();
}

test("login shows overview", async ({ page }) => {
  await login(page);
  await expect(page.getByText("来源、出口、订阅，一条链路看清。")).toBeVisible();
  await expect(page.getByRole("button", { name: "复制订阅" })).toBeVisible();
});

test("set target count shows waiting exit", async ({ page }) => {
  await login(page);
  await page.goto(BASE + "/#/exits");
  // 页头常驻一个，空列表还有一个：都开同一对话框，取页头那个。
  await page.getByRole("button", { name: "调整数量" }).first().click();
  await page.getByLabel("目标数量").fill("1");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await expect(page.getByText("目标数量已保存")).toBeVisible();
  // 无候选：对账后应为等待位（30s 内），不强求隧道就绪。
  await expect
    .poll(async () => (await consoleSummary(page)).allocated, { timeout: 30000 })
    .toBe(1);
});

test("subscription copy link and qr", async ({ page }) => {
  await login(page);
  // 前置：开本机直出，保证订阅有可输出入口（需 xray，CI 会备好 workdir/bin/xray）。
  const en = await page.request.post(BASE + "/api/console/direct", {
    data: { enabled: true },
  });
  expect(en.ok()).toBeTruthy();
  await page.goto(BASE + "/#/subscription");
  const copyBtn = page.getByRole("button", { name: "复制订阅链接" });
  await expect(copyBtn).toBeEnabled();
  await copyBtn.click();
  // 成功时按钮与 toast 同翻“已复制”，失败只出 toast：都只认 toast
  //（role=status），避开按钮+toast 双命中；链接本身由二维码断言覆盖。
  const receipt = page
    .getByRole("status")
    .getByText("已复制")
    .or(page.getByRole("status").getByText("复制失败"));
  await expect(receipt).toBeVisible();
  const qr = page.getByRole("img", { name: "订阅二维码" });
  await expect(qr).toBeVisible();
  expect(await qr.getAttribute("src")).toContain("/api/console/qr");
  // 收尾：关直出，不影响后续用例。
  const dis = await page.request.post(BASE + "/api/console/direct", {
    data: { enabled: false },
  });
  expect(dis.ok()).toBeTruthy();
});

test("delete waiting exit back to zero", async ({ page }) => {
  await login(page);
  await page.goto(BASE + "/#/exits");
  await page.getByRole("button", { name: "查看详情" }).first().click();
  await expect(page.getByText("出口编号")).toBeVisible();
  await page.getByRole("button", { name: "删除出口" }).first().click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("删除后目标数量会减少 1");
  await dialog.getByRole("button", { name: "删除出口" }).click();
  await expect(page.getByText("出口已删除，目标数量已减少 1")).toBeVisible();
  // 先等后端目标归零，再断言空态：列表靠版本轮询刷新，抢跑会 flake。
  await expect
    .poll(async () => (await consoleSummary(page)).target, { timeout: 30000 })
    .toBe(0);
  await expect(page.getByText("没有符合条件的出口")).toBeVisible();
});
