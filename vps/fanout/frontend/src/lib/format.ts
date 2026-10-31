const regionNames =
  typeof Intl !== "undefined" && "DisplayNames" in Intl
    ? new Intl.DisplayNames(["zh-CN"], { type: "region" })
    : null;

export function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

export function countryName(code: string | undefined | null): string {
  const value = (code || "").toUpperCase();
  if (!/^[A-Z]{2}$/.test(value)) return "地区未知";
  try {
    return regionNames?.of(value) || value;
  } catch {
    return value;
  }
}

export function countryCode(code: string | undefined | null): string {
  const value = (code || "").toUpperCase();
  return /^[A-Z]{2}$/.test(value) ? value : "??";
}

export function formatDateTime(value: string | undefined | null): string {
  if (!value || value.startsWith("0001")) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const now = new Date();
  const sameYear = date.getFullYear() === now.getFullYear();
  return date.toLocaleString("zh-CN", {
    ...(sameYear ? {} : { year: "numeric" }),
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatDateShort(value: string | undefined | null): string {
  if (!value || value.startsWith("0001")) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
}

export function formatRelative(seconds: number | undefined | null): string {
  if (seconds === undefined || seconds === null || !Number.isFinite(seconds)) return "—";
  const value = Math.max(0, Math.round(seconds));
  if (value < 5) return "刚刚";
  if (value < 60) return `${value} 秒前`;
  if (value < 3600) return `${Math.floor(value / 60)} 分钟前`;
  if (value < 86400) return `${Math.floor(value / 3600)} 小时前`;
  return `${Math.floor(value / 86400)} 天前`;
}

export function formatDuration(seconds: number | undefined | null): string {
  if (seconds === undefined || seconds === null || !Number.isFinite(seconds)) return "—";
  const value = Math.max(0, Math.round(seconds));
  if (value < 60) return `${value} 秒`;
  const minutes = Math.floor(value / 60);
  const rest = value % 60;
  if (minutes < 60) return rest ? `${minutes} 分 ${rest} 秒` : `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const remain = minutes % 60;
  return remain ? `${hours} 小时 ${remain} 分` : `${hours} 小时`;
}

export function formatBytes(value: number | undefined | null, digits = 1): string {
  if (!value || !Number.isFinite(value) || value <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index === 0 ? 0 : digits)} ${units[index]}`;
}

export function formatLatency(value: number | undefined | null): string {
  if (value === undefined || value === null || !Number.isFinite(value) || value <= 0) return "—";
  return `${Math.round(value)} ms`;
}

export function formatNumber(value: number | undefined | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return "0";
  return value.toLocaleString("zh-CN");
}

export function formatMbps(value: number | undefined | null): string {
  if (!value || !Number.isFinite(value)) return "—";
  return `${value.toFixed(1)} Mbps`;
}

export function truncate(value: string, max = 36): string {
  if (value.length <= max) return value;
  return `${value.slice(0, Math.max(0, max - 1))}…`;
}
