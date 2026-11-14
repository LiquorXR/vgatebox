import { describe, expect, it } from "vitest";
import {
  countryCode,
  countryName,
  formatDuration,
  formatHitRate,
  formatLatency,
  formatNumber,
  formatRelative,
  formatVersion,
  pad2,
  truncate,
} from "../format";

describe("format", () => {
  it("formats country codes", () => {
    expect(countryName("JP")).toBe("日本");
    expect(countryName("")).toBe("地区未知");
    expect(countryCode("jp")).toBe("JP");
    expect(countryCode("")).toBe("??");
  });

  it("formats numbers and latency", () => {
    expect(pad2(3)).toBe("03");
    expect(formatNumber(1200)).toBe("1,200");
    expect(formatLatency(42.4)).toBe("42 ms");
    expect(formatLatency(0)).toBe("—");
    expect(truncate("abcdefghij", 5)).toBe("abcd…");
  });

  it("formats cache hit rates", () => {
    expect(formatHitRate(131, 8)).toBe("94%");
    expect(formatHitRate(0, 5)).toBe("0%");
    expect(formatHitRate(0, 0)).toBe("—");
    expect(formatHitRate(undefined, undefined)).toBe("—");
  });

  it("formats a single version prefix", () => {
    expect(formatVersion("v1.2.4")).toBe("v1.2.4");
    expect(formatVersion("1.2.4")).toBe("v1.2.4");
    expect(formatVersion("vv1.2.4")).toBe("v1.2.4");
    expect(formatVersion("dev")).toBe("dev");
    expect(formatVersion("")).toBe("dev");
  });

  it("formats relative times and durations", () => {
    expect(formatRelative(2)).toBe("刚刚");
    expect(formatRelative(65)).toBe("1 分钟前");
    expect(formatRelative(3700)).toBe("1 小时前");
    expect(formatDuration(59)).toBe("59 秒");
    expect(formatDuration(125)).toBe("2 分 5 秒");
    expect(formatDuration(3660)).toBe("1 小时 1 分");
  });
});
