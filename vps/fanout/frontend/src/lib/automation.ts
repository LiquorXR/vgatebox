import type { AutomationView, PoolSourceView } from "@/api/types";
import { formatDateTime } from "./format";

const runStatus: Record<string, string> = {
  never: "尚未执行",
  running: "进行中",
  passed: "刷新成功",
  failed: "刷新失败",
  completed: "检测完成",
  cancelled: "已取消",
  interrupted: "上次中断",
  no_candidates: "没有候选",
  invalid: "状态异常",
};

export function scheduleLabel(
  view: AutomationView | null | undefined,
  key: "refresh" | "check",
): string {
  const item = view?.[key];
  if (!item) return "读取中";
  if (!item.enabled) return "已关闭";
  let text = `每 ${item.minutes} 分钟 · ${runStatus[item.status] || item.status}`;
  if (item.running) text += " · 完成后再计时";
  else if (item.waiting_for_refresh) text += " · 等待来源刷新完成";
  else if (item.next_at) text += ` · 下次 ${formatDateTime(item.next_at)}`;
  if (item.finished_at) text += ` · 上次 ${formatDateTime(item.finished_at)}`;
  return text;
}

export function sourceLabel(source: PoolSourceView | undefined | null): string {
  if (!source) return "来源信息读取中";
  let text = source.label || "既有清单";
  if (source.retrieved_at) text += ` · 本机获取 ${formatDateTime(source.retrieved_at)}`;
  if (source.kind === "mirror") {
    if (source.snapshot_fetched_at) text += ` · 镜像快照 ${formatDateTime(source.snapshot_fetched_at)}`;
    if (source.stale) text += "（超过 3 小时，快照较旧）";
    text += source.integrity_verified ? " · 文件完整性已校验" : " · 元数据待核对";
  }
  return text;
}
