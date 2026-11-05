import { beforeEach, describe, expect, it } from "vitest";
import { installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

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
});
