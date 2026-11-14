<script setup lang="ts">
defineProps<{
  label: string;
  value: string | number;
  unit?: string;
  hint?: string;
}>();
</script>

<template>
  <article class="min-w-0 rounded-xl border border-line bg-surface p-4 shadow-sm">
    <div class="flex items-center justify-between gap-2">
      <span class="truncate text-xs text-muted">{{ label }}</span>
      <slot name="icon" />
    </div>
    <div class="mt-2 flex items-baseline gap-1">
      <span class="tnum text-[26px] font-semibold leading-none tracking-tight text-ink">{{ value }}</span>
      <span v-if="unit" class="text-xs font-medium text-faint">{{ unit }}</span>
    </div>
    <p v-if="hint" class="mt-1.5 truncate text-xs text-faint">{{ hint }}</p>
  </article>
</template>
