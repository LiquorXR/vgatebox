import { describe, expect, it } from "vitest";
import { buildSubUrl, countSubEntries } from "../subscription";
import type { Exit, SubInfo } from "@/api/types";

const info: SubInfo = {
  token: "tok",
  path: "/base/sub?token=tok",
  url: "https://panel.example/base/sub?token=tok",
};

describe("buildSubUrl", () => {
  it("uses the default base64 range and includes all sources", () => {
    const url = buildSubUrl(info, "base64", {
      vpnAll: true,
      slots: new Set(),
      direct: true,
      thirdAll: true,
      proxies: new Set(),
    });
    expect(url).toContain("token=tok");
    expect(url).not.toContain("target=");
    expect(url).not.toContain("slots=");
    expect(new URL(url).searchParams.get("third")).toBe("1");
  });

  it("sets Clash format and a selected VPN range", () => {
    const url = buildSubUrl(info, "clash", {
      vpnAll: false,
      slots: new Set([2, 5]),
      direct: false,
      thirdAll: false,
      proxies: new Set([9]),
    });
    const parsed = new URL(url);
    expect(parsed.searchParams.get("target")).toBe("clash");
    expect(parsed.searchParams.get("slots")).toBe("2,5");
    expect(parsed.searchParams.get("direct")).toBe("0");
    expect(parsed.searchParams.get("proxies")).toBe("9");
  });

  it("returns an empty string when nothing is selected", () => {
    expect(
      buildSubUrl(info, "base64", {
        vpnAll: false,
        slots: new Set(),
        direct: false,
        thirdAll: false,
        proxies: new Set(),
      }),
    ).toBe("");
  });
});

describe("countSubEntries", () => {
  it("counts only ready exits and enabled inbounds", () => {
    const exits = [
      {
        slot: 1,
        status: "up",
        inbounds: [
          { id: 1, port: 30001, protocol: "vless", enable: true, managed_slot: true, remark: "", tag: "" },
          { id: 2, port: 30002, protocol: "vless", enable: false, managed_slot: true, remark: "", tag: "" },
        ],
      },
      {
        slot: 2,
        status: "waiting",
        inbounds: [{ id: 3, port: 30003, protocol: "vless", enable: true, managed_slot: true, remark: "", tag: "" }],
      },
    ] as Exit[];
    const counts = countSubEntries(
      exits,
      [{ id: 9, port: 40000, protocol: "vless", enable: true, remark: "", tag: "" }],
      [],
      { vpnAll: true, slots: new Set(), direct: true, thirdAll: true, proxies: new Set() },
    );
    expect(counts.vpnExits).toBe(1);
    expect(counts.vpnInbounds).toBe(1);
    expect(counts.direct).toBe(1);
    expect(counts.custom).toBe(0);
  });
});
