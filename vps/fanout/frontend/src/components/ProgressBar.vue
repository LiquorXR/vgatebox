<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    value: number;
    tone?: "accent" | "ok" | "warn" | "bad" | "info";
    label?: string;
    size?: "sm" | "md";
  }>(),
  { tone: "accent", size: "md" },
);

const percent = computed(() => Math.min(100, Math.max(0, Number.isFinite(props.value) ? props.value : 0)));
const tones = {
  accent: "bg-accent",
  ok: "bg-ok",
  warn: "bg-warn",
  bad: "bg-bad",
  info: "bg-info",
};
</script>

<template>
  <div
    class="w-full overflow-hidden rounded-full border border-line bg-surface-2"
    :class="size === 'sm' ? 'h-1.5' : 'h-2'"
    role="progressbar"
    :aria-valuenow="Math.round(percent)"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-label="label || '进度'"
  >
    <div
      class="h-full rounded-full transition-[width] duration-500"
      :class="tones[tone]"
      :style="{ width: `${percent}%` }"
    />
  </div>
</template>
