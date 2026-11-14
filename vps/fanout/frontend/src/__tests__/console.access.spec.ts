import type { DOMWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { button, dialog, installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

function tab(screen: DOMWrapper<Element>, label: string) {
  const found = screen.findAll('[role="tab"]').find((item) => item.text().includes(label));
  if (!found) throw new Error(`Tab not found: ${label}`);
  return found;
}

describe("console access", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("shows SOCKS5, third-party, SSTP and advanced entries", async () => {
    const { screen } = await mountConsole("/access", api);

    await waitFor(
      () => screen.findAll("button").some((item) => item.text() === "详情"),
      "SOCKS5 table",
    );
    await button(screen, "详情").trigger("click");
    await waitFor(() => dialog("SOCKS5 凭据") !== undefined, "SOCKS5 drawer");
    const socksDrawer = dialog("SOCKS5 凭据")!;
    expect(socksDrawer.text()).toContain("外部连接地址");
    expect(socksDrawer.text()).toContain("服务器本机地址");
    await socksDrawer.get('[aria-label="关闭"]').trigger("click");
    await waitFor(() => dialog("SOCKS5 凭据") === undefined, "SOCKS5 drawer close");

    await tab(screen, "第三方 SOCKS5").trigger("click");
    await waitFor(() => screen.text().includes("美国 · 自有代理"), "third-party tab");

    await tab(screen, "原始 SSTP").trigger("click");
    await waitFor(() => screen.text().includes("public-vpn-102"), "SSTP tab");

    await tab(screen, "高级接入").trigger("click");
    await waitFor(
      () =>
        screen.text().includes("自动入口 · in-1") &&
        screen.text().includes("本机网络 · host-direct"),
      "advanced tab",
    );

    await button(screen, "新增接入入口").trigger("click");
    await waitFor(() => dialog("新增接入入口") !== undefined, "create dialog");
    const create = dialog("新增接入入口")!;
    const selects = create.findAll("select");
    expect(selects.some((item) => item.text().includes("VLESS"))).toBe(true);
    expect((selects[2].element as HTMLSelectElement).value).toBe("xhttp");
    expect((selects[3].element as HTMLSelectElement).value).toBe("reality");
    await button(create, "取消").trigger("click");
    await waitFor(() => dialog("新增接入入口") === undefined, "create dialog close");

    const hostRow = screen.findAll("tr").find((item) => item.text().includes("host-direct"));
    expect(hostRow).toBeDefined();
    await button(hostRow!, "删除").trigger("click");
    await waitFor(() => dialog("删除接入入口") !== undefined, "delete inbound confirmation");
    await button(dialog("删除接入入口")!, "取消").trigger("click");
    await waitFor(() => dialog("删除接入入口") === undefined, "delete inbound close");

    await button(screen, "详情").trigger("click");
    await waitFor(() => dialog("客户端 · 1") !== undefined, "inbound detail");
    const detail = dialog("客户端 · 1")!;
    expect(detail.text()).toContain("client@example");
    await button(detail, "复制到所有就绪出口").trigger("click");
    await waitFor(() => dialog("已有入口不会被删除") !== undefined, "clone confirmation");
    await button(dialog("已有入口不会被删除")!, "复制").trigger("click");

    await waitFor(() => screen.text().includes("已创建 2 个接入入口"), "clone toast");
    const clone = api.calls.find((call) => call.path === "/api/xui/clone");
    expect(clone?.url).toContain("id=101");
  });

  it("deletes a managed exit through the console target API", async () => {
    const { screen } = await mountConsole("/access", api);

    await waitFor(() => screen.text().includes("VPS SOCKS5"), "access page");
    await tab(screen, "高级接入").trigger("click");
    await waitFor(
      () => screen.findAll("tr").some((item) => item.text().includes("自动入口 · in-1")),
      "managed entry",
    );

    const row = screen.findAll("tr").find((item) => item.text().includes("自动入口 · in-1"))!;
    await button(row, "删除出口").trigger("click");
    await waitFor(() => dialog("目标数量会自动减少 1") !== undefined, "managed delete confirmation");
    const confirm = dialog("目标数量会自动减少 1")!;
    await confirm.get('input[name="cleanup-archive"]').setValue(true);
    await button(confirm, "删除出口").trigger("click");

    await waitFor(() => screen.text().includes("自动出口和归档入口已删除"), "managed delete toast");
    const remove = api.calls.find((call) => call.path === "/api/console/slot/delete");
    expect(remove?.body).toEqual({ slot: 1, cleanup_archive: true });
  });

  it("cleans an archived entry without an exit", async () => {
    const { screen } = await mountConsole("/access", api);

    await waitFor(() => screen.text().includes("VPS SOCKS5"), "access page");
    await tab(screen, "高级接入").trigger("click");
    await waitFor(
      () => screen.findAll("tr").some((item) => item.text().includes("in-30006-tcp")),
      "archived entry",
    );

    const row = screen.findAll("tr").find((item) => item.text().includes("in-30006-tcp"))!;
    expect(row.text()).toContain("归档入口");
    await button(row, "清理入口").trigger("click");
    await waitFor(() => dialog("对应出口已不存在") !== undefined, "archived cleanup confirmation");
    await button(dialog("对应出口已不存在")!, "清理").trigger("click");

    await waitFor(() => screen.text().includes("归档入口已清理"), "archived cleanup toast");
    const remove = api.calls.find((call) => call.path === "/api/xui/delete");
    expect(remove?.url).toContain("ids=906");
  });

  it("edits SOCKS5 credentials from the access drawer", async () => {
    const { screen } = await mountConsole("/access", api);

    await waitFor(
      () => screen.findAll("button").some((item) => item.text() === "详情"),
      "SOCKS5 table",
    );
    await button(screen, "详情").trigger("click");
    await waitFor(() => dialog("SOCKS5 凭据") !== undefined, "SOCKS5 drawer");
    const drawer = dialog("SOCKS5 凭据")!;
    await drawer.get('input[name="socks-user"]').setValue("rotated-user");
    await drawer.get('input[name="socks-pass"]').setValue("rotated-pass");
    await button(drawer, "保存凭据").trigger("click");

    await waitFor(() => screen.text().includes("SOCKS5 凭据已保存"), "credential toast");
    const save = api.calls.find((call) => call.path === "/api/cred");
    expect(save?.body).toEqual({ slot: 1, user: "rotated-user", pass: "rotated-pass" });
  });
});
