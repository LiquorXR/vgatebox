<script setup lang="ts">
import { TrendingDown, TrendingUp } from "@lucide/vue";

defineProps<{ trend?: string }>();
</script>

<template>
  <span v-if="trend === 'rising'" title="延迟上升中" class="inline-flex items-center text-bad">
    <TrendingUp :size="12" />
  </span>
  <span v-else-if="trend === 'falling'" title="延迟下降中" class="inline-flex items-center text-ok">
    <TrendingDown :size="12" />
  </span>
</template>
