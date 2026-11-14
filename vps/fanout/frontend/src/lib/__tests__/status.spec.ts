import { describe, expect, it } from "vitest";
import { exitKind, exitName, exitStatusTone, ipTypeTone, sourceResidentialLabel, verifiedCountry } from "../status";
import type { Exit } from "@/api/types";

const exit = (overrides: Partial<Exit> = {}): Exit =>
  ({
    slot: 3,
    port: 30003,
    host: "public-vpn-123",
    region: "JP",
    country: "Japan",
    exit_ip: "203.0.113.9",
    status: "up",
    since: "2026-10-31T00:00:00Z",
    egress: { country: "JP", country_status: "passed" },
    inbounds: [{ id: 1, port: 30003, protocol: "vless", enable: true, managed_slot: true, remark: "", tag: "" }],
    ...overrides,
  }) as Exit;

describe("exit status helpers", () => {
  it("maps statuses to a consistent tone", () => {
    expect(exitStatusTone("up")).toBe("ok");
    expect(exitStatusTone("starting")).toBe("info");
    expect(exitStatusTone("waiting_for_nodes")).toBe("warn");
    expect(exitStatusTone("degraded")).toBe("warn");
    expect(exitStatusTone("blocked")).toBe("bad");
  });

  it("keeps residential and unknown visually distinct", () => {
    expect(ipTypeTone("strict_residential")).toBe("ok");
    expect(ipTypeTone("unknown")).toBe("neutral");
    expect(ipTypeTone(undefined)).toBe("neutral");
  });

  it("does not turn an unknown egress type into a self-operated source", () => {
    expect(sourceResidentialLabel(true)).toBe("志愿者节点");
    expect(sourceResidentialLabel(false)).toBe("VPN Gate 自营");
    expect(sourceResidentialLabel(undefined)).toBe("来源未知");
  });

  it("uses only verified egress country", () => {
    expect(verifiedCountry(exit())).toBe("JP");
    expect(verifiedCountry(exit({ egress: { country: "US", country_status: "failed" } }))).toBe("");
  });

  it("honours a custom inbound name", () => {
    const value = exit({
      inbounds: [
        { id: 1, port: 30003, protocol: "vless", enable: true, managed_slot: true, name_mode: "custom", remark: "东京 · 家宽", tag: "" },
      ],
    });
    expect(exitName(value)).toBe("东京 · 家宽");
  });

  it("detects custom and VPN Gate exits", () => {
    expect(exitKind(exit())).toBe("vpn");
    expect(
      exitKind(
        exit({
          inbounds: [{ id: 2, port: 30004, protocol: "vless", enable: true, remark: "third", tag: "" }],
        }),
      ),
    ).toBe("custom");
  });
});
