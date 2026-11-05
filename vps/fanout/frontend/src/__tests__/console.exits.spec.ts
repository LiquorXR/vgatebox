import { beforeEach, describe, expect, it } from "vitest";
import { keys } from "@/api/queries";
import { button, dialog, installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

describe("console exits", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("keeps policy edits local and swaps the selected node", async () => {
    const { screen, queryClient } = await mountConsole("/exits?slot=1", api);

    await waitFor(() => dialog("日本 · 01") !== undefined, "exit drawer");
    const drawer = dialog("日本 · 01")!;
    expect(drawer.text()).not.toContain("SOCKS5 凭据");

    const country = drawer.get('select[name="policy-country"]');
    const japanOptions = country.findAll("option").filter((option) => option.text().includes("日本"));
    expect(japanOptions).toHaveLength(1);

    const checkbox = drawer.get('input[name="policy-ip-type"]');
    const element = checkbox.element as HTMLInputElement;
    const before = element.checked;
    await checkbox.setValue(!before);
    expect(element.checked).toBe(!before);
    expect(api.calls.filter((call) => call.path === "/api/slot/policy")).toHaveLength(0);

    // The console and exits queries poll; a background refresh must not reset local edits.
    const exitsCalls = api.calls.filter((call) => call.path === "/api/exits").length;
    await queryClient.invalidateQueries({ queryKey: keys.exits });
    await queryClient.invalidateQueries({ queryKey: keys.console });
    await waitFor(
      () => api.calls.filter((call) => call.path === "/api/exits").length > exitsCalls,
      "exits refetch",
    );
    expect(element.checked).toBe(!before);
    expect(api.calls.filter((call) => call.path === "/api/slot/policy")).toHaveLength(0);

    await button(drawer, "更换 IP").trigger("click");
    await waitFor(() => dialog("将断开当前节点并重新挑选一个候选") !== undefined, "swap confirmation");
    const confirm = dialog("将断开当前节点并重新挑选一个候选")!;
    await button(confirm, "更换 IP").trigger("click");

    await waitFor(() => screen.text().includes("正在更换节点；出口编号和端口保持不变"), "swap toast");
    const swap = api.calls.find((call) => call.path === "/api/swap");
    expect(swap?.method).toBe("POST");
    expect(swap?.url).toContain("slot=1");
  });

  it("shows region inventory in the target dialog and saves a new target", async () => {
    const { screen } = await mountConsole("/exits", api);

    await waitFor(() => screen.text().includes("已分配"), "exits page");
    await button(screen, "调整数量").trigger("click");
    await waitFor(() => dialog("调整 VPN Gate 数量") !== undefined, "target dialog");

    const target = dialog("调整 VPN Gate 数量")!;
    const japan = target
      .get('select[name="target-country"]')
      .findAll("option")
      .find((option) => option.text().includes("日本（JP）"));
    expect(japan).toBeDefined();
    expect(japan!.text()).toContain("12 个空闲");
    expect(japan!.text()).toContain("35ms");
    expect(japan!.text()).toContain("48.5Mbps");

    await target.get('input[name="target-count"]').setValue("5");
    await button(target, "保存并应用").trigger("click");

    await waitFor(() => dialog("调整 VPN Gate 数量") === undefined, "target dialog close");
    await waitFor(() => screen.text().includes("目标数量已保存，正在按策略连接"), "target toast");
    const save = api.calls.find((call) => call.path === "/api/console/target");
    expect(save?.body).toMatchObject({
      count: 5,
      policy: { country: "" },
      server_name: "www.tesla.com",
    });
  });

  it("deletes an exit and reduces the target count", async () => {
    const { screen } = await mountConsole("/exits?slot=1", api);

    await waitFor(() => dialog("日本 · 01") !== undefined, "exit drawer");
    await button(dialog("日本 · 01")!, "删除出口").trigger("click");

    await waitFor(() => dialog("删除后目标数量会减少 1") !== undefined, "delete confirmation");
    const confirm = dialog("删除后目标数量会减少 1")!;
    expect(confirm.text()).toContain("同时清理归档入口");
    await button(confirm, "删除出口").trigger("click");

    await waitFor(() => screen.text().includes("出口已删除，目标数量已减少 1"), "delete toast");
    const remove = api.calls.find((call) => call.path === "/api/console/slot/delete");
    expect(remove?.body).toEqual({ slot: 1, cleanup_archive: false });
  });
});
