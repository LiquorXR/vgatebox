import type { Page } from "@playwright/test";

const now = "2026-10-31T10:24:00Z";

const policy = {
  country: "",
  allowed_ip_types: ["strict_residential", "likely_residential", "mobile", "datacenter", "business_isp", "unknown"],
  preferred_isp: [],
  allowed_isp: [],
  blocked_isp: [],
  allowed_asn: [],
  blocked_asn: [],
  maximum_latency_ms: 0,
};

const exits = [
  {
    slot: 1,
    port: 30001,
    host: "public-vpn-101",
    region: "JP",
    country: "JP",
    exit_ip: "203.0.113.11",
    status: "up",
    phase: "",
    err: "",
    since: now,
    egress: { country: "JP", country_status: "passed", intelligence_status: "passed", ip_type: "datacenter" },
    policy,
    latency_ms: 42,
    can_rollback_policy: false,
    public_socks: { port: 40001, enable: true },
    socks_user: "u1",
    socks_pass: "p1",
    inbounds: [
      {
        id: 101,
        port: 31001,
        protocol: "vless",
        enable: true,
        tag: "in-1",
        managed_slot: true,
        bound_slot: 1,
        name_mode: "auto",
        remark: "",
      },
    ],
  },
  {
    slot: 2,
    port: 30002,
    host: "public-vpn-102",
    region: "KR",
    country: "KR",
    exit_ip: "203.0.113.12",
    status: "up",
    phase: "",
    err: "",
    since: now,
    egress: { country: "KR", country_status: "passed", intelligence_status: "passed", ip_type: "datacenter" },
    policy,
    latency_ms: 48,
    can_rollback_policy: false,
    public_socks: { port: 40002, enable: false },
    socks_user: "u2",
    socks_pass: "p2",
    inbounds: [
      {
        id: 102,
        port: 31002,
        protocol: "vless",
        enable: true,
        tag: "in-2",
        managed_slot: true,
        bound_slot: 2,
        name_mode: "auto",
        remark: "",
      },
    ],
  },
  {
    slot: 3,
    port: 30003,
    host: "public-vpn-103",
    region: "US",
    country: "US",
    exit_ip: "203.0.113.13",
    status: "starting",
    phase: "connecting",
    err: "",
    since: now,
    egress: null,
    policy,
    latency_ms: 0,
    can_rollback_policy: false,
    public_socks: null,
    socks_user: "u3",
    socks_pass: "p3",
    inbounds: [],
  },
  {
    slot: 4,
    port: 30004,
    host: "public-vpn-104",
    region: "DE",
    country: "DE",
    exit_ip: "203.0.113.14",
    status: "waiting",
    phase: "",
    err: "",
    since: now,
    egress: null,
    policy,
    latency_ms: 0,
    can_rollback_policy: false,
    public_socks: null,
    socks_user: "u4",
    socks_pass: "p4",
    inbounds: [],
  },
];

const nodes = [
  {
    hostname: "public-vpn-101",
    ip: "198.51.100.1",
    country: "JP",
    country_code: "JP",
    asn: "AS64501",
    isp: "Example Broadband",
    ip_type: "likely_residential",
    expected_exit_ip: "203.0.113.11",
    sstp_candidates: [],
    ping: 35,
    speed_mbps: 24,
    sessions: 1,
    residential: true,
  },
  {
    hostname: "public-vpn-102",
    ip: "198.51.100.2",
    country: "KR",
    country_code: "KR",
    asn: "AS64502",
    isp: "Example Cloud",
    ip_type: "datacenter",
    expected_exit_ip: "203.0.113.12",
    sstp_candidates: [{ host: "public-vpn-102", port: 443 }],
    ping: 40,
    speed_mbps: 20,
    sessions: 0,
    residential: false,
  },
  {
    hostname: "public-vpn-103",
    ip: "198.51.100.3",
    country: "US",
    country_code: "US",
    asn: "AS64503",
    isp: "Example Cloud",
    ip_type: "datacenter",
    expected_exit_ip: "203.0.113.13",
    sstp_candidates: [],
    ping: 45,
    speed_mbps: 18,
    sessions: 0,
    residential: false,
  },
  {
    hostname: "public-vpn-104",
    ip: "198.51.100.4",
    country: "DE",
    country_code: "DE",
    asn: "AS64504",
    isp: "Example Cloud",
    ip_type: "datacenter",
    expected_exit_ip: "203.0.113.14",
    sstp_candidates: [],
    ping: 50,
    speed_mbps: 16,
    sessions: 0,
    residential: false,
  },
];

const results = {
  "public-vpn-101": {
    host: "public-vpn-101",
    status: "passed",
    checked_at: now,
    latency_ms: 42,
    actual_ip: "203.0.113.11",
    country: "JP",
    ip_type: "likely_residential",
    stale: false,
  },
  "public-vpn-102": {
    host: "public-vpn-102",
    status: "failed",
    checked_at: now,
    latency_ms: 0,
    actual_ip: "",
    country: "",
    ip_type: "datacenter",
    stale: false,
    code: "https_unreachable",
  },
};

const consoleData = {
  version: "v0.9.2",
  capacity: 40,
  target: 4,
  target_configured: true,
  suggested_target: 16,
  allocated: 4,
  ready: 2,
  stopped: 0,
  waiting: 1,
  candidates: 4,
  pool_read_at: now,
  pool_generated_at: now,
  pool_source: {
    kind: "mirror",
    label: "第三方镜像 GeorgeXie2333 · 本 VPS 获取",
    retrieved_at: now,
    snapshot_fetched_at: now,
    stale: false,
    integrity_verified: true,
  },
  automation: {
    settings: { refresh_enabled: true, refresh_minutes: 60, check_enabled: true, check_minutes: 60 },
    refresh: {
      enabled: true,
      minutes: 60,
      status: "passed",
      running: false,
      finished_at: now,
      next_at: "2026-10-31T11:24:00Z",
      nodes: 4,
      passed: 0,
    },
    check: {
      enabled: true,
      minutes: 60,
      status: "completed",
      running: false,
      finished_at: now,
      next_at: "2026-10-31T11:24:00Z",
      nodes: 4,
      passed: 2,
    },
    error: "",
  },
  default_server_name: "www.tesla.com",
  plan: { count: 4, policy, server_name: "www.tesla.com" },
  plan_error: "",
  refresh: { status: "passed", checked_at: now, elapsed_seconds: 12, timeout_seconds: 90 },
  check: null,
  check_settings: { parallelism: 2 },
  results,
  memory: { MemTotal: 5368709120, MemAvailable: 3120000000 },
};

const exitsView = {
  exits,
  host_direct: [
    {
      id: 900,
      port: 32000,
      protocol: "vless",
      enable: true,
      tag: "host-direct",
      host_direct: true,
      name_mode: "custom",
      remark: "VPS 本机直出",
    },
  ],
  direct: [],
  panel: "",
  backend: "native",
  panel_info: "内置 Xray",
  public_ip: "203.0.113.2",
};

const thirdParty = {
  proxies: [
    {
      id: 201,
      host: "proxy.example.com",
      port: 1080,
      country: "US",
      city: "Los Angeles",
      remark: "自有代理",
      enabled: true,
      subscribe: true,
      entry_port: 32001,
      name: "美国 · 自有代理",
      check: {
        status: "passed",
        actual_ip: "192.0.2.44",
        latency_ms: 88,
        country: "US",
        city: "Los Angeles",
        ip_type: "datacenter",
        checked_at: now,
        expires_at: now,
      },
    },
  ],
  countries: [
    { code: "JP", name: "日本" },
    { code: "US", name: "美国" },
  ],
};

const sstp = {
  nodes: [
    {
      hostname: "public-vpn-102",
      server_ip: "198.51.100.2",
      source_country: "KR",
      candidates: [{ host: "public-vpn-102", port: 443 }],
      result: {
        host: "public-vpn-102",
        status: "passed",
        port: 443,
        actual_ip: "203.0.113.12",
        country: "KR",
        checked_at: now,
        expires_at: "2026-10-31T11:24:00Z",
        latency_ms: 60,
      },
    },
  ],
  settings: { parallelism: 2, interval_minutes: 0, valid_minutes: 60, timeout_seconds: 90 },
  last_started: now,
  check: null,
  dependency_error: "",
  cf_status: "not_tested",
};

const settings = {
  base_path: "",
  port: 8899,
  listen_addr: "0.0.0.0",
  has_password: true,
  residential_only: true,
  version: "v0.9.2",
};

const sub = { token: "abcdef1234567890", path: "/sub?token=abcdef1234567890", url: "http://127.0.0.1:4173/sub?token=abcdef1234567890" };

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
  "base64",
);

const payloads: Record<string, unknown> = {
  "/api/console": consoleData,
  "/api/exits": exitsView,
  "/api/nodes": { nodes, fetched: now, total: nodes.length, available: nodes.length, residential_only: true },
  "/api/sub": sub,
  "/api/settings": settings,
  "/api/update/status": { status: "idle", message: "尚无更新任务", backup_available: false, rolled_back: false },
  "/api/resources/third-party": thirdParty,
  "/api/resources/sstp": sstp,
  "/api/console/socks": {
    links: [{ slot: 1, name: "日本 · 01", url: "socks5://u1:p1@203.0.113.2:40001" }],
  },
  "/api/console/automation": consoleData.automation,
  "/api/console/source-settings": { mirror_enabled: true, source_timeout_seconds: 10, mirror_max_age_hours: 24 },
};

export async function mockApi(page: Page): Promise<void> {
  await page.route("**/api/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;

    if (path === "/api/console/qr") {
      return route.fulfill({ status: 200, contentType: "image/png", body: png });
    }

    const payload = payloads[path];
    if (payload !== undefined) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(payload),
      });
    }

    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ok: true }),
    });
  });

  await page.route("**/login", async (route) => {
    if (route.request().method() !== "POST") {
      return route.fallback();
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ok: "已登录" }),
    });
  });
}

export { png };
