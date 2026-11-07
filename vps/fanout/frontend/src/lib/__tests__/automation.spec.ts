import { describe, expect, it } from "vitest";
import { postRefreshCheckLabel, scheduleLabel, sourceLabel } from "../automation";
import type { AutomationView } from "@/api/types";

const view: AutomationView = {
  settings: { refresh_enabled: true, refresh_minutes: 60 },
  refresh: {
    enabled: true,
    minutes: 60,
    status: "passed",
    running: false,
    finished_at: "2026-10-31T10:00:00Z",
    next_at: "2026-10-31T11:00:00Z",
    nodes: 12,
    passed: 0,
  },
  check: {
    enabled: false,
    minutes: 30,
    status: "completed",
    running: false,
  },
  error: "",
};

describe("scheduleLabel", () => {
  it("shows interval, status, next and last run", () => {
    const text = scheduleLabel(view, "refresh");
    expect(text).toContain("每 60 分钟");
    expect(text).toContain("刷新成功");
    expect(text).toContain("下次");
    expect(text).toContain("上次");
  });

  it("shows a disabled schedule", () => {
    expect(scheduleLabel(view, "check")).toBe("已关闭");
  });

  it("does not show a next run while running", () => {
    const running = {
      ...view,
      refresh: { ...view.refresh, status: "running", running: true, next_at: "2026-10-31T11:00:00Z" },
    };
    const text = scheduleLabel(running, "refresh");
    expect(text).toContain("完成后再计时");
    expect(text).not.toContain("下次");
  });

  it("shows that a check waits for the source refresh", () => {
    const waiting = {
      ...view,
      check: { ...view.check, enabled: true, waiting_for_refresh: true, next_at: "2026-10-31T11:00:00Z" },
    };
    const text = scheduleLabel(waiting, "check");
    expect(text).toContain("等待来源刷新完成");
    expect(text).not.toContain("下次");
  });

  it("shows the post-refresh full check status", () => {
    expect(postRefreshCheckLabel(view)).toContain("刷新成功后自动全量");
    const done = { ...view, check: { ...view.check, finished_at: "2026-10-31T11:00:00Z", nodes: 4, passed: 2 } };
    expect(postRefreshCheckLabel(done)).toContain("上次");
    expect(postRefreshCheckLabel(done)).toContain("2/4");
    expect(postRefreshCheckLabel(null)).toBe("读取中");
  });
});

describe("sourceLabel", () => {
  it("shows retrieved and mirror snapshot times", () => {
    const text = sourceLabel({
      kind: "mirror",
      label: "第三方镜像",
      retrieved_at: "2026-10-31T10:00:00Z",
      snapshot_fetched_at: "2026-10-31T09:00:00Z",
      stale: true,
      integrity_verified: true,
    });
    expect(text).toContain("第三方镜像");
    expect(text).toContain("本机获取");
    expect(text).toContain("镜像快照");
    expect(text).toContain("快照较旧");
    expect(text).toContain("文件完整性已校验");
  });

  it("handles a missing source", () => {
    expect(sourceLabel(undefined)).toBe("来源信息读取中");
  });
});
