<script setup lang="ts">
import type { CheckResult, EgressProfile, Node } from "@/api/types";
import DialogShell from "@/components/DialogShell.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { countryName, formatDateTime, formatMbps } from "@/lib/format";
import {
  checkStatusLabel,
  checkStatusTone,
  errorLabel,
  ipTypeLabel,
  sourceResidentialLabel,
} from "@/lib/status";

defineProps<{
  open: boolean;
  node: Node | null;
  result: CheckResult | null;
  egress: EgressProfile | null | undefined;
  ipType: string;
  observationExpiry: string;
  submitting: boolean;
}>();

const emit = defineEmits<{
  "update:open": [boolean];
  check: [];
}>();
</script>

<template>
  <DialogShell
    :open="open"
    :title="node?.hostname || '节点详情'"
    description="来源节点与检测观测"
    size="md"
    @update:open="emit('update:open', $event)"
  >
    <div v-if="node && result" class="space-y-4">
      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
        <div><dt class="text-faint">服务器 IP</dt><dd class="mt-0.5 font-mono">{{ node.ip }}</dd></div>
        <div><dt class="text-faint">来源地区</dt><dd class="mt-0.5">{{ countryName(node.country_code) }}（{{ node.country_code }}）</dd></div>
        <div><dt class="text-faint">ASN</dt><dd class="mt-0.5">{{ node.asn || "—" }}</dd></div>
        <div><dt class="text-faint">ISP</dt><dd class="mt-0.5">{{ node.isp || "—" }}</dd></div>
        <div><dt class="text-faint">来源类型</dt><dd class="mt-0.5">{{ sourceResidentialLabel(node.source_residential) }}</dd></div>
        <div><dt class="text-faint">IP 类型</dt><dd class="mt-0.5">{{ ipTypeLabel(ipType) }}</dd></div>
        <div><dt class="text-faint">类型依据</dt><dd class="mt-0.5">{{ egress?.reason || "—" }}</dd></div>
        <div><dt class="text-faint">画像状态</dt><dd class="mt-0.5">{{ egress?.intelligence_status || "—" }}</dd></div>
        <div><dt class="text-faint">画像来源</dt><dd class="mt-0.5">{{ egress?.provider || "—" }}</dd></div>
        <div><dt class="text-faint">画像有效至</dt><dd class="mt-0.5">{{ formatDateTime(egress?.expires_at) }}</dd></div>
        <div v-if="egress?.fraud_score !== undefined && egress?.fraud_score !== null"><dt class="text-faint">风险分</dt><dd class="tnum mt-0.5">{{ egress.fraud_score }}</dd></div>
        <div><dt class="text-faint">预期出口</dt><dd class="mt-0.5 font-mono">{{ node.expected_exit_ip || "—" }}</dd></div>
        <div><dt class="text-faint">SSTP 候选</dt><dd class="mt-0.5">{{ node.sstp_candidates?.length || 0 }} 个</dd></div>
        <div><dt class="text-faint">Ping</dt><dd class="tnum mt-0.5">{{ node.ping ? `${node.ping} ms` : "—" }}</dd></div>
        <div><dt class="text-faint">速度</dt><dd class="tnum mt-0.5">{{ formatMbps(node.speed_mbps) }}</dd></div>
      </dl>
      <div class="rounded-lg border border-line p-3 text-xs">
        <div class="mb-1 text-faint">当前观测</div>
        <StatusBadge :label="checkStatusLabel(result.status)" :tone="checkStatusTone(result.status)" size="sm" />
        <p v-if="result.code" class="mt-2 text-bad">{{ errorLabel(result.code) }}</p>
        <p class="mt-2 text-faint">检测时间 {{ formatDateTime(result.checked_at) }}</p>
        <p class="mt-1 text-faint">结果有效至 {{ formatDateTime(result.expires_at) }}</p>
        <p class="mt-1 text-faint">排序观察有效至 {{ formatDateTime(observationExpiry) }}</p>
      </div>
      <UiButton size="sm" :loading="submitting" @click="emit('check')">立即检测</UiButton>
    </div>
  </DialogShell>
</template>
