<script setup lang="ts">
import { TriangleAlert } from "@lucide/vue";
import DialogShell from "./DialogShell.vue";
import UiButton from "./UiButton.vue";

withDefaults(
  defineProps<{
    open: boolean;
    title: string;
    description: string;
    confirmLabel?: string;
    cancelLabel?: string;
    busy?: boolean;
    tone?: "danger" | "primary";
  }>(),
  { confirmLabel: "确认", cancelLabel: "取消", busy: false, tone: "danger" },
);

const emit = defineEmits<{ "update:open": [value: boolean]; confirm: [] }>();
</script>

<template>
  <DialogShell
    :open="open"
    :title="title"
    :description="description"
    size="sm"
    @update:open="emit('update:open', $event)"
  >
    <div class="flex gap-3 rounded-lg border border-warn/20 bg-warn-soft p-3 text-xs leading-relaxed text-warn">
      <TriangleAlert :size="16" class="mt-0.5 shrink-0" />
      <p>此操作会立即影响正在使用的连接。请确认已经了解影响范围。</p>
    </div>
    <slot />
    <template #footer>
      <UiButton variant="ghost" :disabled="busy" @click="emit('update:open', false)">{{ cancelLabel }}</UiButton>
      <UiButton :variant="tone" :loading="busy" @click="emit('confirm')">{{ confirmLabel }}</UiButton>
    </template>
  </DialogShell>
</template>
