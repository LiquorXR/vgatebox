<script setup lang="ts">
import type { CheckResult, Node } from "@/api/types";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { countryName, formatDateTime, formatLatency, truncate } from "@/lib/format";
import {
  checkStatusLabel,
  checkStatusTone,
  errorLabel,
  ipTypeLabel,
  ipTypeTone,
  sourceResidentialLabel,
} from "@/lib/status";

export interface NodeRowView {
  node: Node;
  result: CheckResult;
  ipType: string;
}

defineProps<{
  rows: NodeRowView[];
  selected: Set<string>;
  submitting: Set<string>;
}>();

const emit = defineEmits<{
  "toggle-node": [host: string, checked: boolean];
  "open-detail": [node: Node];
  "check-node": [node: Node];
}>();
</script>

<template>
  <tr v-for="row in rows" :key="row.node.hostname" class="border-b border-line last:border-b-0 hover:bg-surface-2">
    <td class="px-4 py-2.5">
      <input type="checkbox" name="select-node" :checked="selected.has(row.node.hostname)" :aria-label="`选择 ${row.node.hostname}`" class="accent-accent" @change="emit('toggle-node', row.node.hostname, ($event.target as HTMLInputElement).checked)" />
    </td>
    <td class="px-2 py-2.5">
      <button type="button" class="min-w-0 text-left" @click="emit('open-detail', row.node)">
        <span class="block truncate font-mono text-[11px] font-medium text-ink hover:text-accent">{{ truncate(row.node.hostname, 30) }}</span>
        <span class="block truncate text-[11px] text-faint">{{ row.node.ip }}<template v-if="row.node.asn"> · {{ row.node.asn }}</template><template v-if="row.node.isp"> · {{ truncate(row.node.isp, 20) }}</template></span>
      </button>
    </td>
    <td class="px-2 py-2.5 text-xs">
      <span class="font-medium">{{ countryName(row.node.country_code) }}</span>
      <span class="ml-1 text-[10px] text-faint">{{ row.node.country_code }}</span>
      <div class="text-[10px] text-faint">{{ sourceResidentialLabel(row.node.source_residential) }}</div>
    </td>
    <td class="px-2 py-2.5 text-xs">
      <span class="block font-mono text-[11px]">{{ row.result.actual_ip || row.node.expected_exit_ip || "—" }}</span>
      <span class="text-[10px] text-faint">{{ row.result.country ? countryName(row.result.country) : "地区未知" }}</span>
    </td>
    <td class="px-2 py-2.5">
      <StatusBadge :label="ipTypeLabel(row.ipType)" :tone="ipTypeTone(row.ipType)" size="sm" />
    </td>
    <td class="px-2 py-2.5">
      <StatusBadge :label="checkStatusLabel(row.result.status)" :tone="checkStatusTone(row.result.status)" size="sm" :pulse="row.result.status === 'running'" />
      <div v-if="row.result.code" class="mt-1 text-[10px] text-bad">{{ errorLabel(row.result.code) }}</div>
    </td>
    <td class="tnum px-2 py-2.5 text-xs text-muted">{{ formatLatency(row.result.latency_ms) }}</td>
    <td class="px-2 py-2.5 text-[11px] text-faint">
      {{ formatDateTime(row.result.checked_at) }}
      <div class="text-[10px]">有效至 {{ formatDateTime(row.result.expires_at) }}</div>
    </td>
    <td class="px-4 py-2.5 text-right">
      <UiButton size="sm" variant="ghost" :loading="submitting.has(row.node.hostname)" @click="emit('check-node', row.node)">检测</UiButton>
    </td>
  </tr>
</template>
