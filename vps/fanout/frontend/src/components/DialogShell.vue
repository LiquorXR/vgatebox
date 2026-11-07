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
import { ref } from "vue";
import { shouldDismissDrawer } from "@/lib/gestures";
import UiButton from "./UiButton.vue";

const props = withDefaults(
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

// 抽屉下滑关闭：只在内容滚到顶且向下起手时跟手，保护列表横滑与内部滚动。
const contentRef = ref<HTMLElement | null>(null);
const dragY = ref(0);
const dragging = ref(false);
let startY = 0;
let startT = 0;

function onTouchStart(e: TouchEvent) {
  if (props.variant !== "drawer") return;
  startY = e.touches[0]?.clientY ?? 0;
  startT = performance.now();
  dragging.value = false;
  dragY.value = 0;
}

function onTouchMove(e: TouchEvent) {
  if (props.variant !== "drawer") return;
  const dy = (e.touches[0]?.clientY ?? startY) - startY;
  if (!dragging.value) {
    if (dy < 8 || (contentRef.value?.scrollTop ?? 0) > 0) return;
    dragging.value = true;
  }
  dragY.value = Math.max(0, dy);
}

function onTouchEnd() {
  if (!dragging.value) return;
  const dismiss = shouldDismissDrawer(dragY.value, performance.now() - startT);
  dragging.value = false;
  dragY.value = 0;
  if (dismiss) emit("update:open", false);
}

// iOS 键盘弹起不改变 layout viewport，fixed 抽屉下半的输入框会被盖住。
// body 被 Reka 锁定时，只滚动抽屉内容区即可。
function keepFieldVisible(e: FocusEvent) {
  const el = e.target;
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) {
    el.scrollIntoView({ block: "nearest" });
  }
}
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-ink/40 backdrop-blur-[2px] data-[state=open]:animate-in" />
      <DialogContent
        class="fixed z-50 flex flex-col border-line bg-surface shadow-lg focus:outline-none"
        :class="
          variant === 'drawer'
            ? 'inset-y-0 right-0 h-dvh w-full max-w-md border-l animate-drawer-in'
            : ['inset-x-3 top-1/2 mx-auto max-h-[calc(100dvh-2rem)] -translate-y-1/2 rounded-xl border', widths[size]]
        "
        :style="dragging ? { transform: `translateY(${dragY}px)` } : undefined"
        @touchstart.passive="onTouchStart"
        @touchmove.passive="onTouchMove"
        @touchend="onTouchEnd"
      >
        <div v-if="variant === 'drawer'" class="flex justify-center pt-2.5 sm:hidden" aria-hidden="true">
          <span class="h-1 w-10 rounded-full bg-line-strong" />
        </div>
        <header class="flex items-start justify-between gap-3 border-b border-line px-4 py-3.5">
          <div class="min-w-0">
            <DialogTitle class="text-base font-semibold tracking-tight text-ink">{{ title }}</DialogTitle>
            <DialogDescription :class="description ? 'mt-0.5 text-xs text-muted' : 'sr-only'">
              {{ description || title }}
            </DialogDescription>
          </div>
          <DialogClose as-child>
            <UiButton variant="ghost" size="sm" aria-label="关闭" class="h-10 w-10 shrink-0 sm:h-8 sm:w-auto">
              <X :size="16" />
            </UiButton>
          </DialogClose>
        </header>
        <div
          ref="contentRef"
          class="scrollbar-thin min-h-0 flex-1 overscroll-contain overflow-y-auto px-4 py-4"
          @focusin="keepFieldVisible"
        >
          <slot />
        </div>
        <footer
          v-if="$slots.footer"
          class="flex flex-col items-stretch gap-2 border-t border-line bg-surface-2 px-4 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:justify-end"
        >
          <slot name="footer" />
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
