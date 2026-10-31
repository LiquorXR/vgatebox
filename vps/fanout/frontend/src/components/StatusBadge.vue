<script setup lang="ts">
import { computed } from "vue";
import type { Tone } from "@/lib/status";

const props = withDefaults(
  defineProps<{
    label: string;
    tone?: Tone;
    pulse?: boolean;
    size?: "sm" | "md";
  }>(),
  { tone: "neutral", pulse: false, size: "md" },
);

const tones: Record<Tone, string> = {
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warn",
  bad: "bg-bad-soft text-bad",
  info: "bg-info-soft text-info",
  neutral: "bg-surface-3 text-muted",
};

const classes = computed(() => [
  "inline-flex w-fit items-center gap-1.5 rounded-full font-medium whitespace-nowrap",
  props.size === "sm" ? "px-2 py-0.5 text-[11px]" : "px-2.5 py-1 text-xs",
  tones[props.tone],
]);
</script>

<template>
  <span :class="classes">
    <span
      class="size-1.5 shrink-0 rounded-full bg-current"
      :class="pulse ? 'animate-pulse' : ''"
      aria-hidden="true"
    />
    {{ label }}
  </span>
</template>
