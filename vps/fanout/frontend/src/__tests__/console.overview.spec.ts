import { beforeEach, describe, expect, it } from "vitest";
import { button, installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

describe("console overview", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("loads the console summary and navigates to the exits page", async () => {
    const { screen } = await mountConsole("/", api);

    await waitFor(() => screen.findAll("h1").some((item) => item.text() === "总览"), "overview heading");
    expect(screen.text()).toContain("就绪出口");
    expect(screen.text()).toContain("2 / 4");
    expect(screen.text()).not.toContain("vv0.9.2");
    expect(screen.text()).toContain("v0.9.2");
    expect(screen.findAll("button").filter((item) => item.text().includes("刷新来源"))).toHaveLength(1);
    expect(screen.findAll("button").filter((item) => item.text().includes("复制订阅"))).toHaveLength(1);

    const nav = screen.get('nav[aria-label="主导航"]');
    const exitsLink = nav.findAll("a").find((item) => item.attributes("href") === "/exits");
    expect(exitsLink).toBeDefined();
    await exitsLink!.trigger("click");

    await waitFor(() => screen.findAll("h1").some((item) => item.text() === "出口"), "exits heading");
    expect(screen.text()).toContain("日本 · 01");
    expect(screen.text()).toContain("韩国 · 02");
  });

  it("renders the mobile navigation for every console section", async () => {
    const { screen } = await mountConsole("/", api);

    await waitFor(() => screen.findAll("h1").some((item) => item.text() === "总览"), "overview heading");
    const nav = screen.get('nav[aria-label="移动端主导航"]');
    const links = nav.findAll("a");
    expect(links.map((link) => link.text())).toEqual(["总览", "出口", "节点池", "订阅", "接入", "设置"]);
    expect(links.map((link) => link.attributes("href"))).toEqual([
      "/",
      "/exits",
      "/nodes",
      "/subscription",
      "/access",
      "/settings",
    ]);
    expect(nav.get('a[aria-current="page"]').text()).toBe("总览");
  });
});
