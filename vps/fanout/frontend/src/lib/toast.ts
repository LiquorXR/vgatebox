import { ref } from "vue";

export type ToastTone = "info" | "success" | "error";

export interface ToastItem {
  id: number;
  message: string;
  tone: ToastTone;
  actionLabel?: string;
  action?: () => void | Promise<void>;
}

const items = ref<ToastItem[]>([]);
let sequence = 0;

export const toasts = items;

export function dismissToast(id: number): void {
  items.value = items.value.filter((item) => item.id !== id);
}

export function toast(
  message: string,
  options: { tone?: ToastTone; actionLabel?: string; action?: () => void | Promise<void>; duration?: number } = {},
): number {
  const id = ++sequence;
  items.value = [...items.value.slice(-3), { id, message, tone: options.tone || "info", actionLabel: options.actionLabel, action: options.action }];
  const duration = options.duration ?? (options.action ? 9000 : 4500);
  window.setTimeout(() => dismissToast(id), duration);
  return id;
}

export function toastError(error: unknown, fallback = "操作失败"): void {
  const message = error instanceof Error && error.message ? error.message : fallback;
  toast(message, { tone: "error" });
}
