import type { CheckResult, Exit, ExitInbound, Node } from "@/api/types";
import { countryCode, countryName, pad2 } from "./format";

export type Tone = "ok" | "warn" | "bad" | "info" | "neutral";

const exitLabels: Record<string, string> = {
  up: "已就绪",
  starting: "连接中",
  waiting: "等待候选",
  waiting_for_nodes: "等待候选",
  blocked: "已阻断",
  stopped: "已停止",
  error: "连接异常",
};

export function exitStatusLabel(status: string): string {
  return exitLabels[status] || status || "未知";
}

export function exitStatusTone(status: string): Tone {
  if (status === "up") return "ok";
  if (status === "starting") return "info";
  if (status === "waiting" || status === "waiting_for_nodes") return "warn";
  if (status === "blocked" || status === "error") return "bad";
  return "neutral";
}

const checkLabels: Record<string, string> = {
  passed: "检测通过",
  failed: "未通过",
  not_tested: "未检测",
  stale: "结果已过期",
  expired: "结果已过期",
  queued: "排队中",
  priority_queued: "优先等待",
  running: "检测中",
  submitting: "提交中",
  done: "已完成",
  cancelled: "已取消",
  deferred: "暂缓检测",
};

export function checkStatusLabel(status: string): string {
  return checkLabels[status] || status || "未检测";
}

export function checkStatusTone(status: string): Tone {
  if (status === "passed") return "ok";
  if (status === "failed") return "bad";
  if (status === "running" || status === "queued" || status === "priority_queued" || status === "submitting") return "info";
  if (status === "deferred" || status === "stale" || status === "expired") return "warn";
  return "neutral";
}

export const phaseLabels: Record<string, string> = {
  waiting_capacity: "等待建连名额",
  precheck: "检查端口",
  setup: "准备临时环境",
  connecting: "连接节点",
  baseline: "确认基础网络",
  profile: "查询出口画像",
  verify_exit: "核验实际出口",
  waiting_exit: "等待出口就绪",
  cleanup: "清理临时环境",
};

export const errorLabels: Record<string, string> = {
  check_timeout: "检测超时",
  openvpn_timeout: "连接节点超时",
  openvpn_failed: "连接节点失败",
  evidence_unavailable: "画像证据暂不可用",
  exit_ip_mismatch: "实际出口不一致，已阻断",
  probe_environment_failed: "检测环境初始化失败",
  https_unreachable: "HTTPS 暂不可达",
  exit_not_ready: "出口尚未就绪",
  policy_type_mismatch: "IP 类型不符合策略",
  policy_latency_exceeded: "超过延迟上限",
  cancelled: "已取消",
  pool_refresh_failed_previous_pool_retained: "来源刷新失败，保留旧清单",
  allocation_failed: "出口分配等待重试",
  default_initialization_failed: "默认出口初始化等待重试",
  auto_binding_failed: "接入绑定等待重试",
  tcp_unreachable: "TCP 端口暂不可达",
  network_capacity_exhausted: "实例地址容量已用完",
  candidate_in_use: "节点正在被占用",
  exit_recovery_priority: "优先恢复出口，稍后重试",
  candidate_changed: "节点已变化，等待重新检测",
  remove_failed: "出口移除等待重试",
  check_cancelled: "检测已取消",
  result_save_failed: "结果保存失败",
  pool_refresh_timeout: "来源刷新超时",
  pool_refresh_busy: "已有刷新任务在进行",
  pool_source_unavailable: "来源暂时不可达",
  pool_source_no_matching_candidates: "没有符合筛选条件的候选",
  mirror_integrity_failed: "镜像文件完整性校验失败",
  mirror_snapshot_expired: "镜像快照已过期",
  mirror_download_failed: "镜像下载失败",
  pool_refresh_input_error: "来源配置无效",
  pool_refresh_failed: "来源刷新失败",
};

export function errorLabel(code: string | undefined | null): string {
  if (!code) return "";
  return errorLabels[code] || code;
}

export const ipTypeLabels: Record<string, string> = {
  strict_residential: "住宅 IP",
  unknown: "类型未知",
};

export function ipTypeLabel(value: string | undefined | null): string {
  if (!value) return "类型未知";
  return ipTypeLabels[value] || value;
}

export function ipTypeTone(value: string | undefined | null): Tone {
  return value === "strict_residential" ? "ok" : "neutral";
}

export function sourceResidentialLabel(value: boolean | undefined | null): string {
  if (value === true) return "志愿者节点";
  if (value === false) return "VPN Gate 自营";
  return "来源未知";
}

export function verifiedCountry(exit: Exit): string {
  const egress = exit.egress;
  if (!egress) return "";
  if (egress.country_status === "passed" || egress.intelligence_status === "passed") {
    return (egress.country || "").toUpperCase();
  }
  return "";
}

export function exitCountry(exit: Exit): string {
  return verifiedCountry(exit) || (exit.region || "").toUpperCase();
}

export function exitName(exit: Exit): string {
  const enabled = (exit.inbounds || []).filter((item) => item.enable);
  if (enabled.length === 1 && enabled[0].name_mode === "custom" && enabled[0].remark) {
    return enabled[0].remark;
  }
  return `${countryName(exitCountry(exit))} · ${pad2(exit.slot)}`;
}

export function exitSubLabel(exit: Exit): string {
  const kind = exitKind(exit);
  if (kind === "custom") return "自定义代理 · 经本 VPS 转发";
  const code = exitCountry(exit);
  return `${countryName(code)} · VPN Gate 节点`;
}

export type ExitKind = "vpn" | "custom" | "direct";

export function exitKind(exit: Exit): ExitKind {
  const inbounds = exit.inbounds || [];
  if (inbounds.some((item) => !item.managed_slot && !item.host_direct)) return "custom";
  return "vpn";
}

export function exitKindLabel(kind: ExitKind): string {
  if (kind === "direct") return "本机直出";
  if (kind === "custom") return "自定义代理";
  return "VPN Gate";
}

export function directEnabled(rows: ExitInbound[] | undefined): boolean {
  return (rows || []).some((item) => item.enable);
}

export function directPorts(rows: ExitInbound[] | undefined): number[] {
  return (rows || []).filter((item) => item.enable).map((item) => item.port);
}

export function nodeCheck(node: Node, results: Record<string, CheckResult> | undefined): CheckResult {
  const result = results?.[node.hostname];
  if (!result) return { host: node.hostname, status: "not_tested" };
  return result.stale ? { ...result, status: "stale" } : result;
}

export function exitCheck(exit: Exit, results: Record<string, CheckResult> | undefined): CheckResult {
  const result = results?.[exit.host];
  if (!result) return { host: exit.host, slot: exit.slot, status: "not_tested" };
  return result.stale ? { ...result, status: "stale" } : result;
}

export function countryTag(exit: Exit): string {
  const code = exitCountry(exit);
  return code ? countryCode(code) : "??";
}
