<script setup lang="ts">
defineProps<{
  title: string;
  description?: string;
  compact?: boolean;
}>();
</script>

<template>
  <div
    class="flex flex-col items-center justify-center rounded-xl border border-dashed border-line-strong bg-surface-2/60 px-5 text-center"
    :class="compact ? 'py-7' : 'py-12'"
  >
    <div v-if="$slots.icon" class="mb-3 text-faint">
      <slot name="icon" />
    </div>
    <p class="text-sm font-medium text-ink">{{ title }}</p>
    <p v-if="description" class="mt-1 max-w-sm text-xs leading-relaxed text-muted">{{ description }}</p>
    <div v-if="$slots.action" class="mt-4">
      <slot name="action" />
    </div>
  </div>
</template>
