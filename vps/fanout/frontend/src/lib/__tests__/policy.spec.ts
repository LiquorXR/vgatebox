import { describe, expect, it } from "vitest";
import { ALL_IP_TYPES, clonePolicy, defaultPolicy, normalizePolicyTypes } from "@/lib/policy";

describe("slot policy helpers", () => {
  it("maps legacy residential values to strict_residential", () => {
    expect(normalizePolicyTypes(["residential", "strict_residential"])).toEqual(["strict_residential"]);
  });

  it("falls back to unknown when no type is selected", () => {
    expect(normalizePolicyTypes([])).toEqual(["unknown"]);
    expect(normalizePolicyTypes(undefined)).toEqual(["unknown"]);
  });

  it("defaults to every known type and no restrictions", () => {
    const policy = defaultPolicy("JP");
    expect(policy.country).toBe("JP");
    expect(policy.allowed_ip_types).toEqual(ALL_IP_TYPES);
    expect(policy.maximum_latency_ms).toBe(0);
  });

  it("clones nested arrays instead of sharing them", () => {
    const source = defaultPolicy("US");
    source.allowed_ip_types = ["strict_residential"];
    const copy = clonePolicy(source);
    copy.allowed_ip_types.push("unknown");
    copy.blocked_isp.push("Example ISP");
    expect(source.allowed_ip_types).toEqual(["strict_residential"]);
    expect(source.blocked_isp).toEqual([]);
  });
});
