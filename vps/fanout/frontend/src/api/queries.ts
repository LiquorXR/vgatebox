import { useQuery, useQueryClient } from "@tanstack/vue-query";
import { computed, ref, toValue, watch, type MaybeRefOrGetter } from "vue";
import { api } from "./client";
import type {
  ConsoleSummary,
  ConsoleVersion,
  ExitsView,
  Inbound,
  NodesView,
  SettingsInfo,
  SocksLinksView,
  SstpView,
  SubInfo,
  ThirdPartyView,
  UpdateJob,
} from "./types";

export const keys = {
  console: ["console"] as const,
  consoleVersion: ["console-version"] as const,
  exits: ["exits"] as const,
  nodes: ["nodes"] as const,
  sub: ["sub"] as const,
  settings: ["settings"] as const,
  update: ["update-status"] as const,
  thirdParty: ["third-party"] as const,
  sstp: ["sstp"] as const,
  socks: ["socks-links"] as const,
  inbounds: ["inbounds"] as const,
};

const enabledRef = (value: MaybeRefOrGetter<boolean>) => computed(() => toValue(value));

export function useConsoleQuery() {
  const queryClient = useQueryClient();
  // 上次全量拉取对应的版本号：只在全量成功后更新，避免“拉取途中又递增”
  // 导致的漏更新（兜底的 60s 全量轮询可收敛）。
  const lastFullVersion = ref<number | null>(null);
  const versionQuery = useQuery({
    queryKey: keys.consoleVersion,
    queryFn: () => api<ConsoleVersion>("api/console/version"),
    // 运行期（检测/刷新中）保持 2s，与原来全量轮询的加速档一致；
    // 空闲 5s。从 console 缓存读运行态，不多发请求。
    refetchInterval: () => {
      const c = queryClient.getQueryData<ConsoleSummary>(keys.console);
      return c?.check?.status === "running" || c?.refresh?.status === "running" ? 2000 : 5000;
    },
    refetchIntervalInBackground: false,
    staleTime: 1500,
  });
  const consoleQuery = useQuery({
    queryKey: keys.console,
    queryFn: async () => {
      const data = await api<ConsoleSummary>("api/console");
      // 应答自带的版本号即这份快照的版本，直接对齐，避免多一次全量。
      if (data.console_version != null) lastFullVersion.value = data.console_version;
      return data;
    },
    // 兜底：内存占用、stale 翻转等时间驱动字段不递增版本，60s 全量收敛一次。
    // 空闲流量相对原来下降约 92%（5s 全量 → 5s 小版本 + 60s 全量）。
    refetchInterval: 60000,
    refetchIntervalInBackground: false,
    staleTime: 1500,
  });
  watch(
    () => versionQuery.data.value?.version,
    (v) => {
      if (v == null) return;
      // 首屏：全量查询自己的初始拉取会覆盖，跳过以免重复拉两次。
      if (lastFullVersion.value === null && !consoleQuery.data.value) {
        lastFullVersion.value = v;
        return;
      }
      if (v !== lastFullVersion.value) {
        lastFullVersion.value = v;
        void consoleQuery.refetch();
      }
    },
  );
  return consoleQuery;
}

export function useExitsQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.exits,
    queryFn: () => api<ExitsView>("api/exits"),
    enabled: enabledRef(enabled),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
    staleTime: 1500,
  });
}

export function useNodesQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.nodes,
    queryFn: () => api<NodesView>("api/nodes?all=1"),
    enabled: enabledRef(enabled),
    refetchInterval: 20000,
    refetchIntervalInBackground: false,
    staleTime: 5000,
  });
}

export function useSubQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.sub,
    queryFn: () => api<SubInfo>("api/sub"),
    enabled: enabledRef(enabled),
    staleTime: 60_000,
  });
}

export function useSettingsQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.settings,
    queryFn: () => api<SettingsInfo>("api/settings"),
    enabled: enabledRef(enabled),
    staleTime: 30_000,
  });
}

export function useUpdateStatusQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.update,
    queryFn: () => api<UpdateJob>("api/update/status"),
    enabled: enabledRef(enabled),
    refetchInterval: 4000,
    refetchIntervalInBackground: false,
  });
}

export function useThirdPartyQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.thirdParty,
    queryFn: () => api<ThirdPartyView>("api/resources/third-party"),
    enabled: enabledRef(enabled),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}

export function useSocksLinksQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.socks,
    queryFn: () => api<SocksLinksView>("api/console/socks"),
    enabled: enabledRef(enabled),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}

export function useInboundsQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.inbounds,
    queryFn: () => api<Inbound[]>("api/xui/inbounds"),
    enabled: enabledRef(enabled),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}

export function useSstpQuery(enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: keys.sstp,
    queryFn: () => api<SstpView>("api/resources/sstp"),
    enabled: enabledRef(enabled),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}
