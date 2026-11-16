import { onMounted, onUnmounted, ref } from "vue";
import type { Exit } from "@/api/types";
import { formatRelative } from "./format";

/** A shared 1s clock for UI countdowns that must stay live between polls. */
export function useNow(intervalMs = 1000) {
  const nowMs = ref(Date.now());
  let timer: ReturnType<typeof setInterval> | null = null;
  onMounted(() => {
    timer = setInterval(() => {
      nowMs.value = Date.now();
    }, intervalMs);
  });
  onUnmounted(() => {
    if (timer) clearInterval(timer);
  });
  return nowMs;
}

/** Remaining seconds for waiting_for_nodes; expired or missing values return null. */
export function retryInSeconds(exit: Exit, nowMs: number): number | null {
  if (exit.status !== "waiting_for_nodes" || !exit.retry_at) return null;
  const at = new Date(exit.retry_at).getTime();
  if (!Number.isFinite(at)) return null;
  const left = Math.ceil((at - nowMs) / 1000);
  return left > 0 ? left : null;
}

export function retryText(exit: Exit, nowMs: number): string | null {
  const left = retryInSeconds(exit, nowMs);
  return left === null ? null : `${left} 秒后重试`;
}

/** Previous usable exit, hidden when the current exit already has an IP. */
export function lastExitText(exit: Exit, nowMs: number): string | null {
  if (!exit.last_exit_ip || exit.exit_ip) return null;
  if (!exit.last_exit_at) return `上次 ${exit.last_exit_ip}`;
  const at = new Date(exit.last_exit_at).getTime();
  if (!Number.isFinite(at)) return `上次 ${exit.last_exit_ip}`;
  return `上次 ${exit.last_exit_ip} · ${formatRelative((nowMs - at) / 1000)}`;
}
