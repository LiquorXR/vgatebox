import { useQuery } from "@tanstack/vue-query";
import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { api } from "./client";
import type {
  ConsoleSummary,
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
  return useQuery({
    queryKey: keys.console,
    queryFn: () => api<ConsoleSummary>("api/console"),
    refetchInterval: (query) => {
      const data = query.state.data as ConsoleSummary | undefined;
      return data?.check?.status === "running" || data?.refresh?.status === "running" ? 2000 : 5000;
    },
    refetchIntervalInBackground: false,
    staleTime: 1500,
  });
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
