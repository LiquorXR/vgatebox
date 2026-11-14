import { beforeEach, describe, expect, it } from "vitest";
import { button, dialog, installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

// 回归：抽屉内触发的确认框（如出口页更换 IP）必须压在抽屉上层，
// 不能依赖模板里的 DOM 先后顺序。对话框层级高于抽屉层级。
describe("dialog layering over drawer", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("keeps the swap confirmation above the exit drawer", async () => {
    await mountConsole("/exits?slot=1", api);

    await waitFor(() => dialog("日本 · 01") !== undefined, "exit drawer");
    const drawer = dialog("日本 · 01")!;
    expect(drawer.element.className).toContain("z-50");

    await button(drawer, "更换 IP").trigger("click");
    await waitFor(() => dialog("将断开当前节点并重新挑选一个候选") !== undefined, "swap confirmation");
    const confirm = dialog("将断开当前节点并重新挑选一个候选")!;
    expect(confirm.element.className).toContain("z-[80]");
  });
});
