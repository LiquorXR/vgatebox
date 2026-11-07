<script setup lang="ts">
import { CircleAlert, CircleCheck, Info, X } from "@lucide/vue";
import { dismissToast, toasts, type ToastItem } from "@/lib/toast";

const icons = {
  info: Info,
  success: CircleCheck,
  error: CircleAlert,
};

const tones = {
  info: "text-info",
  success: "text-ok",
  error: "text-bad",
};

function runAction(item: ToastItem) {
  void item.action?.();
  dismissToast(item.id);
}
</script>

<template>
  <div class="pointer-events-none fixed inset-x-0 bottom-[calc(4.25rem+env(safe-area-inset-bottom))] z-[60] flex flex-col items-center gap-2 px-4 sm:inset-x-auto sm:bottom-4 sm:right-5 sm:items-end">
    <TransitionGroup
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="translate-y-2 opacity-0"
      enter-to-class="translate-y-0 opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-for="item in toasts"
        :key="item.id"
        class="pointer-events-auto flex w-full max-w-sm items-start gap-2.5 rounded-xl border border-line bg-surface px-3.5 py-3 shadow-lg"
        :role="item.tone === 'error' ? 'alert' : 'status'"
      >
        <component :is="icons[item.tone]" :size="17" class="mt-0.5 shrink-0" :class="tones[item.tone]" />
        <p class="min-w-0 flex-1 break-words text-sm text-ink">{{ item.message }}</p>
        <button
          v-if="item.actionLabel"
          type="button"
          class="shrink-0 text-xs font-semibold text-accent hover:underline"
          @click="runAction(item)"
        >
          {{ item.actionLabel }}
        </button>
        <button
          type="button"
          class="shrink-0 rounded p-0.5 text-faint hover:bg-surface-3 hover:text-ink"
          aria-label="关闭提示"
          @click="dismissToast(item.id)"
        >
          <X :size="14" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
