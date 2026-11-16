import type { Exit, Inbound } from "@/api/types";
import { directInboundName, exitName, exitStatusLabel, exitStatusTone } from "./status";

export function ownerOf(inbound: Inbound, exits: Exit[]): Exit | null {
  if (inbound.bound_slot) return exits.find((exit) => exit.slot === inbound.bound_slot) || null;
  if (inbound.bound_to) return exits.find((exit) => exit.host === inbound.bound_to) || null;
  return null;
}

export function inboundName(inbound: Inbound, exits: Exit[]): string {
  const owner = ownerOf(inbound, exits);
  if (inbound.managed_slot) {
    if (owner) return exitName(owner);
    return inbound.remark ? `${inbound.remark} · 归档` : "归档入口";
  }
  if (inbound.host_direct) return directInboundName(inbound);
  return inbound.remark || `${inbound.protocol.toUpperCase()} · ${inbound.port}`;
}

export function inboundKind(inbound: Inbound, exits: Exit[]): string {
  if (inbound.host_direct) return "本机网络";
  if (inbound.managed_slot) return ownerOf(inbound, exits) ? "自动入口" : "归档入口";
  if (inbound.manual) return "手动入口";
  return "接入入口";
}

export function inboundState(
  inbound: Inbound,
  exits: Exit[],
): { label: string; tone: "ok" | "warn" | "bad" | "info" | "neutral" } {
  if (inbound.managed_slot && !ownerOf(inbound, exits)) return { label: "出口已删除 · 归档阻断", tone: "bad" };
  if (!inbound.enable) return { label: "已停用", tone: "neutral" };
  if (inbound.host_direct) return { label: "已启用", tone: "ok" };
  const owner = ownerOf(inbound, exits);
  if (!owner) return { label: "未绑定 · 阻断", tone: "bad" };
  return { label: exitStatusLabel(owner.status), tone: exitStatusTone(owner.status) };
}

export function bindValue(inbound: Inbound): string {
  return `slot:${inbound.bound_slot || 0}`;
}

export function bindLabel(inbound: Inbound, exits: Exit[]): string {
  const owner = ownerOf(inbound, exits);
  if (inbound.host_direct) return "独立使用服务器网络";
  if (inbound.managed_slot) {
    if (owner) return `随出口 ${exitName(owner)}`;
    const slot = inbound.bound_slot ? `出口 ${String(inbound.bound_slot).padStart(2, "0")}` : "出口";
    return `${slot} 已删除 · 归档`;
  }
  return owner ? `${exitName(owner)} · ${exitStatusLabel(owner.status)}` : "未绑定（阻断）";
}
