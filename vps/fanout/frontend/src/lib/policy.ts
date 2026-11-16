import type { SlotPolicy } from "@/api/types";
import { ipTypeLabels } from "./status";

export const ALL_IP_TYPES = Object.keys(ipTypeLabels);

export function normalizePolicyTypes(types: string[] | undefined): string[] {
  const out = new Set<string>();
  for (const type of types || []) {
    if (type === "strict_residential" || type === "residential") out.add("strict_residential");
    else out.add("unknown");
  }
  if (!out.size) out.add("unknown");
  return [...out];
}

export function clonePolicy(value: SlotPolicy): SlotPolicy {
  const copy = JSON.parse(JSON.stringify(value)) as SlotPolicy;
  copy.allowed_ip_types = normalizePolicyTypes(copy.allowed_ip_types);
  return copy;
}

export function defaultPolicy(country = ""): SlotPolicy {
  return {
    country,
    allowed_ip_types: [...ALL_IP_TYPES],
    preferred_isp: [],
    allowed_isp: [],
    blocked_isp: [],
    allowed_asn: [],
    blocked_asn: [],
    maximum_latency_ms: 0,
  };
}
