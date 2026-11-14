export class ApiError extends Error {
  status: number;
  payload: unknown;

  constructor(message: string, status: number, payload?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
  }
}

export function resolveRoot(pathname: string = window.location.pathname): string {
  const path = pathname;
  if (path.endsWith("/next/")) return path.slice(0, -"next/".length);
  if (path.endsWith("/next")) return path.slice(0, -"next".length);
  return path.replace(/[^/]*$/, "");
}

/** Path prefix shared by the management UI and its API, e.g. `/aB3x/`. */
export const appRoot = resolveRoot();

export function appUrl(path = ""): string {
  return appRoot + path.replace(/^\//, "");
}

export function apiUrl(path: string): string {
  return appRoot + path.replace(/^\//, "");
}

function errorMessage(payload: unknown, status: number): string {
  if (payload && typeof payload === "object") {
    const record = payload as Record<string, unknown>;
    if (typeof record.error === "string" && record.error) return record.error;
    if (typeof record.message === "string" && record.message) return record.message;
  }
  if (typeof payload === "string" && payload.trim()) return payload.trim().slice(0, 240);
  if (status === 401) return "登录已过期，请重新登录";
  if (status === 403) return "没有权限执行此操作";
  if (status === 404) return "请求的资源不存在";
  if (status >= 500) return "服务暂时不可用，请稍后重试";
  return `请求失败（${status}）`;
}

export interface ApiOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const method = options.method ?? (options.body !== undefined ? "POST" : "GET");
  const headers: Record<string, string> = { Accept: "application/json", ...options.headers };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";

  let response: Response;
  try {
    response = await fetch(apiUrl(path), {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      signal: options.signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new ApiError("无法连接到管理服务，请检查网络后重试", 0);
  }

  const type = response.headers.get("content-type") || "";
  const payload = type.includes("application/json")
    ? await response.json().catch(() => null)
    : await response.text().catch(() => "");

  if (response.status === 401) {
    window.location.reload();
    throw new ApiError("登录已过期，请重新登录", 401, payload);
  }
  if (!response.ok) throw new ApiError(errorMessage(payload, response.status), response.status, payload);
  return payload as T;
}

export function post<T>(path: string, body: unknown = {}): Promise<T> {
  return api<T>(path, { method: "POST", body });
}

export function queryString(values: Record<string, string | number | boolean | undefined | null>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value === undefined || value === null || value === "") continue;
    params.set(key, String(value));
  }
  const text = params.toString();
  return text ? `?${text}` : "";
}
