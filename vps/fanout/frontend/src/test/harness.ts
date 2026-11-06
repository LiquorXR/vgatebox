import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import {
  DOMWrapper,
  flushPromises,
  mount,
  type VueWrapper,
} from "@vue/test-utils";
import { nextTick } from "vue";
import { createMemoryHistory, type Router } from "vue-router";
import { afterEach, vi } from "vitest";
import App from "@/App.vue";
import { toasts } from "@/lib/toast";
import { createAppRouter } from "@/router";
import { apiPayloads, nodesView, qrPngBase64 } from "./fixtures";

export interface ApiCall {
  path: string;
  method: string;
  body: unknown;
  url: string;
}

export interface ApiMock {
  calls: ApiCall[];
  responses: Record<string, unknown>;
}

export interface ConsoleMount {
  wrapper: VueWrapper;
  screen: DOMWrapper<Element>;
  router: Router;
  queryClient: QueryClient;
  api: ApiMock;
}

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function parseBody(init: RequestInit): unknown {
  if (typeof init.body !== "string" || !init.body) return undefined;
  try {
    return JSON.parse(init.body);
  } catch {
    return init.body;
  }
}

function requestUrl(input: RequestInfo | URL): URL {
  if (input instanceof URL) return new URL(input.href);
  if (typeof input === "string") return new URL(input, window.location.href);
  return new URL(input.url, window.location.href);
}

export function installApiMock(overrides: Record<string, unknown> = {}): ApiMock {
  const calls: ApiCall[] = [];
  const responses = { ...apiPayloads, ...overrides };

  const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = requestUrl(input);
    const method = (init.method || "GET").toUpperCase();
    const body = parseBody(init);
    calls.push({ path: url.pathname, method, body, url: url.href });

    if (url.pathname === "/api/console/qr") {
      const bytes = Uint8Array.from(atob(qrPngBase64), (char) => char.charCodeAt(0));
      return new Response(bytes, { status: 200, headers: { "Content-Type": "image/png" } });
    }

    if (url.pathname === "/login" && method === "POST") {
      return jsonResponse({ ok: "已登录" });
    }

    if (url.pathname === "/api/cred" && method === "POST") {
      const payload = (body || {}) as { user?: string; pass?: string };
      return jsonResponse({
        user: payload.user || "generated-user",
        pass: payload.pass || "generated-pass",
      });
    }

    if (url.pathname === "/api/xui/clone" && method === "POST") {
      return jsonResponse({ created: [31002, 31003] });
    }

    if (url.pathname === "/api/sub" && url.searchParams.get("target") === "links") {
      return new Response("vless://example", { status: 200, headers: { "Content-Type": "text/plain" } });
    }

    // /api/nodes 按真实后端语义支持 limit/offset：不带 limit 返回完整 payload，
    // 与旧行为一致；带 limit 按 next_offset 切片。
    if (url.pathname === "/api/nodes") {
      const base = responses[url.pathname] as { nodes?: unknown[] } | undefined;
      const list = base?.nodes ?? nodesView.nodes;
      const limitRaw = url.searchParams.get("limit");
      if (limitRaw === null) return jsonResponse(responses[url.pathname] ?? nodesView);
      const limit = Number(limitRaw);
      const offset = Number(url.searchParams.get("offset") ?? "0");
      const page = list.slice(offset, offset + limit);
      const end = offset + page.length;
      return jsonResponse({
        nodes: page,
        fetched: nodesView.fetched,
        total: list.length,
        available: list.length,
        residential_only: false,
        limit,
        offset,
        next_offset: end < list.length ? end : null,
      });
    }

    const payload = responses[url.pathname];
    if (payload !== undefined) return jsonResponse(payload);

    return jsonResponse({ ok: true });
  });

  vi.stubGlobal("fetch", fetchMock);
  toasts.value = [];
  localStorage.clear();
  document.documentElement.className = "";

  return { calls, responses };
}

let activeWrapper: VueWrapper | null = null;
let activeQueryClient: QueryClient | null = null;

export async function mountConsole(path: string, api: ApiMock): Promise<ConsoleMount> {
  const router = createAppRouter(createMemoryHistory());
  await router.push(path);
  await router.isReady();

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  });

  const wrapper = mount(App, {
    attachTo: document.body,
    global: {
      plugins: [router, [VueQueryPlugin, { queryClient }]],
    },
  });

  activeWrapper = wrapper;
  activeQueryClient = queryClient;
  await flushPromises();

  return { wrapper, screen: new DOMWrapper(document.body), router, queryClient, api };
}

export async function waitFor(
  predicate: () => boolean,
  description = "condition",
  timeout = 3000,
): Promise<void> {
  const deadline = Date.now() + timeout;
  for (;;) {
    await flushPromises();
    await nextTick();
    if (predicate()) return;
    if (Date.now() > deadline) {
      throw new Error(`Timed out waiting for ${description}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

export function dialog(text?: string) {
  const dialogs = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].map(
    (element) => new DOMWrapper(element),
  );
  if (text === undefined) return dialogs[0];
  return dialogs.find((item) => item.text().includes(text));
}

export function button(wrapper: VueWrapper | DOMWrapper<Element>, label: string) {
  const found = wrapper.findAll("button").find((item) => item.text().includes(label));
  if (!found) throw new Error(`Button not found: ${label}`);
  return found;
}

afterEach(() => {
  activeWrapper?.unmount();
  activeWrapper = null;
  activeQueryClient?.clear();
  activeQueryClient = null;
  vi.unstubAllGlobals();
  toasts.value = [];
  document.body.innerHTML = "";
});
