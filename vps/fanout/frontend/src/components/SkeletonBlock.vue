<script setup lang="ts">
withDefaults(defineProps<{ lines?: number; height?: string }>(), { lines: 3, height: "0.75rem" });
</script>

<template>
  <div class="animate-pulse space-y-2.5" aria-hidden="true">
    <div
      v-for="index in lines"
      :key="index"
      class="rounded bg-surface-3"
      :style="{ height, width: index === lines ? '62%' : '100%' }"
    />
  </div>
</template>
