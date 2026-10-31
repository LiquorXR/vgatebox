<script setup lang="ts">
defineProps<{
  title: string;
  description?: string;
  kicker?: string;
  tone?: "neutral" | "ok" | "warn" | "bad" | "info";
}>();
</script>

<template>
  <header class="flex flex-wrap items-end justify-between gap-4">
    <div class="min-w-0">
      <div v-if="kicker" class="mb-1 flex items-center gap-2 text-xs text-muted">
        <span
          class="size-1.5 rounded-full"
          :class="{
            'bg-ok': tone === 'ok',
            'bg-warn': tone === 'warn',
            'bg-bad': tone === 'bad',
            'bg-info': tone === 'info',
            'bg-muted': !tone || tone === 'neutral',
          }"
          aria-hidden="true"
        />
        {{ kicker }}
      </div>
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">{{ title }}</h1>
      <p v-if="description" class="mt-1.5 max-w-2xl text-sm text-muted">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </header>
</template>
