export interface EgressProfile {
  ip?: string;
  country?: string;
  city?: string;
  country_status?: string;
  intelligence_status?: string;
  ip_type?: string;
  protocol?: string;
  provider?: string;
  asn?: string;
  isp?: string;
  expires_at?: string;
  reason?: string;
  fraud_score?: number;
  [key: string]: unknown;
}

export interface SlotPolicy {
  country: string;
  allowed_ip_types: string[];
  preferred_isp: string[];
  allowed_isp: string[];
  blocked_isp: string[];
  allowed_asn: string[];
  blocked_asn: string[];
  maximum_latency_ms: number;
}

export interface ExitPlan {
  count: number;
  policy: SlotPolicy;
  server_name: string;
  remove_slot?: number;
}

export interface SocksExposure {
  port: number;
  enable: boolean;
}

export interface ExitInbound {
  id: number;
  port: number;
  remark: string;
  protocol: string;
  enable: boolean;
  tag: string;
  managed_slot?: boolean;
  manual?: boolean;
  host_direct?: boolean;
  bound_slot?: number;
  name_mode?: string;
}

export interface Exit {
  slot: number;
  port: number;
  host: string;
  region: string;
  country: string;
  exit_ip: string;
  phase?: string;
  phase_elapsed_s?: number;
  pending_swap?: boolean;
  status: string;
  err?: string;
  since: string;
  egress?: EgressProfile | null;
  policy?: SlotPolicy;
  latency_ms?: number;
  latency_trend?: string;
  can_rollback_policy?: boolean;
  public_socks?: SocksExposure | null;
  socks_user?: string;
  socks_pass?: string;
  inbounds?: ExitInbound[];
}

export interface ExitsView {
  exits: Exit[];
  host_direct: ExitInbound[];
  direct: ExitInbound[];
  panel: string;
  backend: string;
  panel_info: string;
  public_ip: string;
}

export interface SstpEndpoint {
  host: string;
  port: number;
}

export interface Node {
  hostname: string;
  ip: string;
  country: string;
  country_code: string;
  asn?: string;
  isp?: string;
  ip_type?: string;
  expected_exit_ip?: string;
  sstp_candidates?: SstpEndpoint[];
  sstp_status?: string;
  openvpn_status?: string;
  openvpn_egress?: EgressProfile;
  ping: number;
  speed_mbps: number;
  sessions: number;
  // 来源标签：志愿者节点或 VPN Gate 自营，不等同于出口住宅类型。
  source_residential?: boolean;
  // 出口画像结论：strict_residential 或 unknown。
  residential: boolean;
  pool_node_id?: string;
}

export interface NodesView {
  nodes: Node[];
  fetched: string;
  total: number;
  available: number;
  residential_only: boolean;
  // 分页字段只在带 limit 请求时出现；不带 limit 时保持旧形状。
  limit?: number;
  offset?: number;
  next_offset?: number | null;
}

export interface CheckResult {
  host: string;
  slot?: number;
  status: string;
  phase?: string;
  code?: string;
  protocol?: string;
  port?: number;
  actual_ip?: string;
  country?: string;
  ip_type?: string;
  latency_ms?: number;
  checked_at?: string;
  expires_at?: string;
  observed_by?: string;
  cf_status?: string;
  stale?: boolean;
  priority?: boolean;
  egress?: EgressProfile | null;
  started_at?: string;
  profile_sha256?: string;
}

export interface CheckRunView {
  id: string;
  status: string;
  kind: string;
  items: CheckResult[];
  done: number;
  total: number;
  parallelism: number;
  ip_type_check?: boolean;
}

export interface RefreshView {
  status: string;
  started_at?: string;
  checked_at?: string;
  error?: string;
  cause?: string;
  elapsed_seconds?: number;
  timeout_seconds?: number;
  submitting?: boolean;
  previous_started_at?: string;
}

export interface PoolSourceView {
  kind: string;
  label: string;
  retrieved_at?: string;
  snapshot_fetched_at?: string;
  data_commit?: string;
  stale?: boolean;
  integrity_verified?: boolean;
}

export interface ScheduleView {
  enabled: boolean;
  minutes: number;
  status: string;
  running: boolean;
  started_at?: string;
  finished_at?: string;
  next_at?: string;
  nodes?: number;
  passed?: number;
  waiting_for_refresh?: boolean;
}

export interface IPTypeCacheView {
  entries: number;
  hits: number;
  queries: number;
}

export interface CheckSettingsView {
  parallelism: number;
  ip_type_check: boolean;
  check_result_ttl_effective_minutes?: number;
  observation_ttl_minutes?: number;
  observation_ttl_effective_minutes?: number;
  check_cycle_seconds?: number;
  ip_type_ttl_hours?: number;
  ip_type_cache?: IPTypeCacheView;
}

export interface AutomationView {
  settings: {
    refresh_enabled: boolean;
    refresh_minutes: number;
  };
  refresh: ScheduleView;
  check: ScheduleView;
  error?: string;
}

export interface RegionStat {
  code: string;
  name: string;
  available: number;
  best_ping: number;
  best_speed_mbps: number;
}

export interface ConsoleSummary {
  version: string;
  console_version?: number;
  capacity: number;
  target: number;
  target_configured: boolean;
  suggested_target: number;
  allocated: number;
  ready: number;
  stopped: number;
  waiting: number;
  candidates: number;
  pool_read_at: string;
  pool_generated_at: string;
  pool_source: PoolSourceView;
  automation: AutomationView | null;
  default_server_name: string;
  plan: ExitPlan | null;
  plan_error: string;
  refresh: RefreshView;
  check: CheckRunView | null;
  check_settings: CheckSettingsView;
  results: Record<string, CheckResult>;
  regions: RegionStat[];
  memory: Record<string, number>;
}

// GET /api/console/version 的小响应：只有单调版本号。
export interface ConsoleVersion {
  version: number;
}

export interface SubInfo {
  token: string;
  path: string;
  url: string;
}

export interface SettingsInfo {
  base_path: string;
  port: number;
  listen_addr: string;
  has_password: boolean;
  residential_only: boolean;
  version: string;
}

export interface UpdateStatus {
  current: string;
  latest: string;
  has_update: boolean;
  notes: string;
  url: string;
  supported: boolean;
}

export interface UpdateJob {
  id?: string;
  status: string;
  phase?: string;
  target?: string;
  message: string;
  started_at?: number;
  updated_at?: number;
  backup_available?: boolean;
  rolled_back?: boolean;
}

export interface ThirdPartyCheck {
  status: string;
  code?: string;
  actual_ip?: string;
  latency_ms?: number;
  country?: string;
  city?: string;
  ip_type: string;
  checked_at?: string;
  expires_at?: string;
}

export interface ThirdPartyRow {
  id: number;
  host: string;
  port: number;
  country: string;
  city: string;
  remark: string;
  enabled: boolean;
  subscribe: boolean;
  entry_port: number;
  name: string;
  check: ThirdPartyCheck;
  country_mismatch?: boolean;
  city_mismatch?: boolean;
}

export interface ThirdPartyProxy {
  host: string;
  port: number;
  username: string;
  password?: string;
  country: string;
  city: string;
  remark: string;
  subscribe: boolean;
}

export interface ThirdPartyView {
  proxies: ThirdPartyRow[];
  countries: { code: string; name: string }[];
}

export interface SstpSettings {
  parallelism: number;
  interval_minutes: number;
  valid_minutes: number;
  timeout_seconds: number;
}

export interface SstpRow {
  hostname: string;
  server_ip: string;
  source_country: string;
  candidates: SstpEndpoint[];
  result: CheckResult;
  attempt?: CheckResult;
}

export interface Inbound {
  id: number;
  port: number;
  protocol: string;
  remark: string;
  enable: boolean;
  tag: string;
  bound_to?: string;
  bound_slot?: number;
  name_mode?: string;
  managed_slot?: boolean;
  manual?: boolean;
  host_direct?: boolean;
  bound_up?: boolean;
}

export interface InboundClient {
  email: string;
  id: string;
  enable: boolean;
}

export interface InboundDetail extends Inbound {
  clients: InboundClient[];
  links: string[];
  listen: string;
  network: string;
  tls: string;
  vless_encryption?: boolean;
  vless_encryption_type?: string;
}

export interface SocksLink {
  slot: number;
  name: string;
  url: string;
}

export interface SocksLinksView {
  links: SocksLink[];
}

export interface SstpView {
  nodes: SstpRow[];
  settings: SstpSettings;
  last_started: string;
  check: CheckRunView | null;
  dependency_error: string;
  cf_status: string;
}
