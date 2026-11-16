<script setup lang="ts">
import type { CheckRunView } from "@/api/types";
import ProgressBar from "@/components/ProgressBar.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import { checkStatusLabel, checkStatusTone } from "@/lib/status";

defineProps<{
  check: CheckRunView | null | undefined;
  percent: number;
}>();
</script>

<template>
  <div v-if="check" class="border-b border-line bg-surface-2 px-4 py-3">
    <div class="flex items-center justify-between gap-2 text-xs">
      <span class="font-medium">候选检测 · {{ check.done }} / {{ check.total }}</span>
      <StatusBadge :label="checkStatusLabel(check.status)" :tone="checkStatusTone(check.status)" size="sm" :pulse="check.status === 'running'" />
    </div>
    <ProgressBar :value="percent" size="sm" class="mt-2" label="候选检测进度" />
  </div>
</template>
