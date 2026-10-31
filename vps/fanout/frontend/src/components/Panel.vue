<script setup lang="ts">
defineProps<{
  title?: string;
  description?: string;
  flush?: boolean;
}>();
</script>

<template>
  <section class="min-w-0 overflow-hidden rounded-xl border border-line bg-surface shadow-sm">
    <header
      v-if="title || $slots.header || $slots.actions"
      class="flex items-start justify-between gap-3 border-b border-line px-4 py-3"
      :class="flush ? 'border-b-0 pb-0' : ''"
    >
      <slot name="header">
        <div class="min-w-0">
          <h2 class="text-sm font-semibold text-ink">{{ title }}</h2>
          <p v-if="description" class="mt-0.5 text-xs text-muted">{{ description }}</p>
        </div>
      </slot>
      <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </header>
    <div :class="flush ? 'p-0' : 'p-4'">
      <slot />
    </div>
  </section>
</template>
