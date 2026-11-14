import { apiUrl } from "@/api/client";
import type { Exit, ExitInbound, SubInfo, ThirdPartyRow } from "@/api/types";

export type SubFormat = "base64" | "clash";

export interface SubScope {
  vpnAll: boolean;
  slots: Set<number>;
  direct: boolean;
  thirdAll: boolean;
  proxies: Set<number>;
}

export function buildSubUrl(info: SubInfo | undefined, format: SubFormat, scope: SubScope): string {
  if (!info?.url) return "";
  const directUsable = scope.direct;
  const vpnSelection = [...scope.slots];
  const proxySelection = [...scope.proxies];
  const hasAny = scope.vpnAll || vpnSelection.length > 0 || directUsable || scope.thirdAll || proxySelection.length > 0;
  if (!hasAny) return "";

  const url = new URL(info.url, window.location.href);
  for (const key of ["slots", "vpn", "direct", "proxies", "third", "target"]) url.searchParams.delete(key);
  if (format === "clash") url.searchParams.set("target", "clash");
  if (!directUsable) url.searchParams.set("direct", "0");
  else if (!scope.vpnAll) url.searchParams.set("direct", "1");

  if (!scope.vpnAll) {
    if (vpnSelection.length) url.searchParams.set("slots", vpnSelection.join(","));
    else url.searchParams.set("vpn", "0");
  }

  if (scope.thirdAll) url.searchParams.set("third", "1");
  else if (proxySelection.length) url.searchParams.set("proxies", proxySelection.join(","));
  else url.searchParams.set("third", "0");

  return url.href;
}

export function subQrUrl(fullUrl: string): string {
  if (!fullUrl) return "";
  const url = new URL(fullUrl);
  url.searchParams.delete("token");
  return apiUrl(`api/console/qr?${url.searchParams.toString()}`);
}

export function countSubEntries(
  exits: Exit[],
  hostDirect: ExitInbound[],
  third: ThirdPartyRow[],
  scope: SubScope,
): { vpnExits: number; vpnInbounds: number; direct: number; custom: number } {
  const selectedExits = exits.filter((exit) => exit.status === "up" && (scope.vpnAll || scope.slots.has(exit.slot)));
  const vpnInbounds = selectedExits.reduce(
    (total, exit) => total + (exit.inbounds || []).filter((inbound) => inbound.enable).length,
    0,
  );
  const direct = scope.direct && hostDirect.some((inbound) => inbound.enable) ? 1 : 0;
  const custom = third.filter(
    (proxy) => proxy.enabled && proxy.subscribe && (scope.thirdAll || scope.proxies.has(proxy.id)),
  ).length;
  return { vpnExits: selectedExits.length, vpnInbounds, direct, custom };
}
