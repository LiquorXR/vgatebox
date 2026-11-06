import { beforeEach, describe, expect, it } from "vitest";
import { button, installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

describe("console nodes", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("filters the candidate table by search term", async () => {
    const { screen } = await mountConsole("/nodes", api);

    await waitFor(() => screen.findAll("h1").some((item) => item.text() === "节点池"), "nodes heading");
    expect(screen.text()).toContain("public-vpn-101");
    expect(screen.text()).toContain("public-vpn-102");

    await screen.get('input[name="node-search"]').setValue("101");
    await waitFor(() => !screen.text().includes("public-vpn-102"), "filtered candidates");
    expect(screen.text()).toContain("public-vpn-101");
  });

  it("loads more candidates in 200-sized server pages", async () => {
    const big = Array.from({ length: 250 }, (_, i) => ({
      hostname: `node-${String(i).padStart(3, "0")}`,
      ip: "198.51.100.9",
      country: "JP",
      country_code: "JP",
      ping: 1,
      speed_mbps: 1,
      sessions: 0,
      residential: false,
    }));
    api.responses["/api/nodes"] = { nodes: big };
    const { screen } = await mountConsole("/nodes", api);

    await waitFor(() => screen.text().includes("已加载 200 / 共 250"), "first page");
    expect(screen.text()).not.toContain("node-249");

    await button(screen, "加载更多候选").trigger("click");
    // 续拉完成后面板收起（再无下一页），总数回到客户端分页条。
    await waitFor(() => !screen.text().includes("加载更多候选"), "load more done");
    expect(screen.text()).toContain("共 250 个候选");
  });
});
