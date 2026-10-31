<script setup lang="ts">
export interface TabItem {
  key: string;
  label: string;
  count?: number;
}

defineProps<{
  items: TabItem[];
  modelValue: string;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
</script>

<template>
  <div class="flex flex-wrap gap-1 rounded-lg border border-line bg-surface-2 p-1" role="tablist">
    <button
      v-for="item in items"
      :key="item.key"
      type="button"
      role="tab"
      :aria-selected="modelValue === item.key"
      class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
      :class="
        modelValue === item.key
          ? 'bg-surface text-ink shadow-sm'
          : 'text-muted hover:bg-surface-3 hover:text-ink'
      "
      @click="emit('update:modelValue', item.key)"
    >
      {{ item.label }}
      <span
        v-if="item.count !== undefined"
        class="tnum rounded-full px-1.5 py-0.5 text-[10px]"
        :class="modelValue === item.key ? 'bg-accent-soft text-accent' : 'bg-surface-3 text-faint'"
      >
        {{ item.count }}
      </span>
    </button>
  </div>
</template>
