<script setup lang="ts">
import { X } from "@lucide/vue";
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from "reka-ui";
import UiButton from "./UiButton.vue";

withDefaults(
  defineProps<{
    open: boolean;
    title: string;
    description?: string;
    variant?: "dialog" | "drawer";
    size?: "sm" | "md" | "lg" | "xl";
  }>(),
  { variant: "dialog", size: "md" },
);

const emit = defineEmits<{ "update:open": [value: boolean] }>();

const widths = {
  sm: "max-w-sm",
  md: "max-w-lg",
  lg: "max-w-2xl",
  xl: "max-w-4xl",
};
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-ink/40 backdrop-blur-[2px] data-[state=open]:animate-in" />
      <DialogContent
        class="fixed z-50 flex flex-col border-line bg-surface shadow-lg focus:outline-none"
        :class="
          variant === 'drawer'
            ? 'inset-y-0 right-0 h-dvh w-full max-w-md border-l data-[state=open]:animate-in'
            : ['inset-x-3 top-1/2 mx-auto max-h-[calc(100dvh-2rem)] -translate-y-1/2 rounded-xl border', widths[size]]
        "
      >
        <header class="flex items-start justify-between gap-3 border-b border-line px-4 py-3.5">
          <div class="min-w-0">
            <DialogTitle class="text-base font-semibold tracking-tight text-ink">{{ title }}</DialogTitle>
            <DialogDescription :class="description ? 'mt-0.5 text-xs text-muted' : 'sr-only'">
              {{ description || title }}
            </DialogDescription>
          </div>
          <DialogClose as-child>
            <UiButton variant="ghost" size="sm" aria-label="关闭">
              <X :size="16" />
            </UiButton>
          </DialogClose>
        </header>
        <div class="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-4 py-4">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="flex items-center justify-end gap-2 border-t border-line bg-surface-2 px-4 py-3">
          <slot name="footer" />
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
