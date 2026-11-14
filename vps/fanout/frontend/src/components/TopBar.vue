<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useConsoleQuery } from "@/api/queries";
import { formatDateTime } from "@/lib/format";
import StatusBadge from "./StatusBadge.vue";
import ThemeMenu from "./ThemeMenu.vue";

const route = useRoute();
const { data, isError, dataUpdatedAt } = useConsoleQuery();

const title = computed(() => String(route.meta.title || "控制台"));
const health = computed(() => {
  if (isError.value) return { label: "连接中断", tone: "bad" as const };
  if (!data.value) return { label: "正在连接", tone: "neutral" as const };
  if (data.value.plan_error) return { label: "计划等待重试", tone: "warn" as const };
  if (!data.value.candidates) return { label: "等待候选", tone: "warn" as const };
  return { label: "运行正常", tone: "ok" as const };
});
const dotClass = computed(
  () =>
    ({
      ok: "bg-ok",
      warn: "bg-warn",
      bad: "bg-bad",
      info: "bg-info",
      neutral: "bg-faint",
    })[health.value.tone],
);
const updated = computed(() => (dataUpdatedAt.value ? formatDateTime(new Date(dataUpdatedAt.value).toISOString()) : "—"));
</script>

<template>
  <header
    class="sticky top-0 z-20 flex h-14 items-center justify-between gap-3 border-b border-line bg-surface/85 px-4 backdrop-blur sm:px-6 lg:px-8"
  >
    <div class="flex min-w-0 items-center gap-3">
      <RouterLink to="/" class="flex items-center gap-2 lg:hidden" aria-label="vgatebox 总览">
        <span class="grid size-7 place-items-center rounded-md bg-accent text-accent-ink" aria-hidden="true">
          <svg viewBox="0 0 16 16" fill="none" class="size-3.5">
            <path
              d="M3 4.2 8 8l5-3.8M3 8.4 8 12.2l5-3.8"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </span>
      </RouterLink>
      <div class="hidden items-center gap-2 text-xs text-muted lg:flex">
        <span>控制台</span>
        <span aria-hidden="true">/</span>
        <span class="font-medium text-ink">{{ title }}</span>
      </div>
      <div class="truncate text-sm font-semibold text-ink lg:hidden">{{ title }}</div>
    </div>

    <div class="flex items-center gap-2">
      <StatusBadge :label="health.label" :tone="health.tone" size="sm" class="hidden sm:inline-flex" />
      <span class="size-2.5 rounded-full sm:hidden" :class="dotClass" role="img" :aria-label="`后端状态：${health.label}`" :title="health.label" />
      <span class="hidden text-[11px] text-faint xl:inline">更新于 {{ updated }}</span>
      <ThemeMenu />
      <span
        class="grid size-7 place-items-center rounded-full bg-accent-soft text-[11px] font-semibold text-accent"
        title="管理员"
      >
        管
      </span>
    </div>
  </header>
</template>
