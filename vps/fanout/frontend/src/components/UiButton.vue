<script setup lang="ts">
import Spinner from "./Spinner.vue";

withDefaults(
  defineProps<{
    variant?: "primary" | "secondary" | "ghost" | "danger";
    size?: "sm" | "md";
    loading?: boolean;
    disabled?: boolean;
    type?: "button" | "submit" | "reset";
  }>(),
  { variant: "secondary", size: "md", loading: false, disabled: false, type: "button" },
);

const base =
  "inline-flex select-none items-center justify-center gap-1.5 rounded-lg font-medium transition-colors disabled:pointer-events-none disabled:opacity-50";
const variants = {
  primary: "bg-accent text-accent-ink hover:bg-accent-hover",
  secondary: "border border-line-strong bg-surface text-ink hover:bg-surface-2",
  ghost: "text-muted hover:bg-surface-3 hover:text-ink",
  danger: "border border-bad/30 bg-bad-soft text-bad hover:border-bad/50",
};
const sizes = {
  sm: "h-8 px-2.5 text-xs",
  md: "h-9 px-3.5 text-sm",
};
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
    :class="[base, variants[variant], sizes[size]]"
  >
    <Spinner v-if="loading" :size="size === 'sm' ? 12 : 14" />
    <slot />
  </button>
</template>
