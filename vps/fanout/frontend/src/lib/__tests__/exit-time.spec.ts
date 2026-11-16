import { describe, expect, it } from "vitest";
import type { Exit } from "@/api/types";
import { lastExitText, retryInSeconds, retryText } from "@/lib/exit-time";

const now = Date.parse("2026-11-16T00:00:00.000Z");

function exit(overrides: Partial<Exit>): Exit {
  return {
    slot: 1,
    port: 30001,
    host: "host",
    region: "JP",
    country: "日本",
    exit_ip: "",
    status: "up",
    since: "2026-11-16T00:00:00.000Z",
    ...overrides,
  } as Exit;
}

describe("exit time helpers", () => {
  it("computes a live retry countdown only for waiting slots", () => {
    const waiting = exit({ status: "waiting_for_nodes", retry_at: "2026-11-16T00:00:45.000Z" });
    expect(retryInSeconds(waiting, now)).toBe(45);
    expect(retryText(waiting, now)).toBe("45 秒后重试");
    expect(retryInSeconds(exit({ status: "up" }), now)).toBeNull();
  });

  it("returns null for expired or malformed retry timestamps", () => {
    expect(retryInSeconds(exit({ status: "waiting_for_nodes", retry_at: "2026-11-15T23:59:00.000Z" }), now)).toBeNull();
    expect(retryInSeconds(exit({ status: "waiting_for_nodes", retry_at: "not-a-date" }), now)).toBeNull();
  });

  it("shows the last exit only when the current exit has no IP", () => {
    const last = exit({ last_exit_ip: "203.0.113.9", last_exit_at: "2026-11-15T23:57:00.000Z" });
    expect(lastExitText(last, now)).toBe("上次 203.0.113.9 · 3 分钟前");
    expect(lastExitText(exit({ exit_ip: "198.51.100.4", last_exit_ip: "203.0.113.9" }), now)).toBeNull();
    expect(lastExitText(exit({}), now)).toBeNull();
  });
});
