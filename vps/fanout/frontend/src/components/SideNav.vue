<script setup lang="ts">
import { computed } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { navGroups, navItems } from "@/lib/nav";
import { useConsoleQuery } from "@/api/queries";
import { formatNumber, formatVersion } from "@/lib/format";
import ProgressBar from "./ProgressBar.vue";

const route = useRoute();
const { data: summary } = useConsoleQuery();

const ready = computed(() => summary.value?.ready ?? 0);
const target = computed(() => summary.value?.target ?? 0);
const candidates = computed(() => summary.value?.candidates ?? 0);
const percent = computed(() => (target.value > 0 ? (ready.value / target.value) * 100 : 0));
const version = computed(() => summary.value?.version || "dev");

function active(to: string) {
  return to === "/" ? route.path === "/" : route.path.startsWith(to);
}

function badgeValue(kind: "ready" | "candidates" | undefined) {
  if (kind === "ready") return ready.value;
  if (kind === "candidates") return candidates.value;
  return 0;
}
</script>

<template>
  <aside class="fixed inset-y-0 left-0 z-30 hidden w-56 flex-col border-r border-line bg-surface lg:flex">
    <div class="flex h-14 items-center gap-2.5 border-b border-line px-4">
      <span class="grid size-8 place-items-center rounded-lg bg-accent text-accent-ink" aria-hidden="true">
        <svg viewBox="0 0 16 16" fill="none" class="size-4">
          <path
            d="M3 4.2 8 8l5-3.8M3 8.4 8 12.2l5-3.8"
            stroke="currentColor"
            stroke-width="1.7"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </span>
      <div class="min-w-0">
        <div class="text-[15px] font-semibold tracking-tight text-ink">vgatebox</div>
        <div class="text-[10px] uppercase tracking-[0.14em] text-faint">Console</div>
      </div>
    </div>

    <nav class="flex-1 overflow-y-auto px-2.5 py-4" aria-label="主导航">
      <div v-for="group in navGroups" :key="group" class="mb-4 last:mb-0">
        <div class="mb-1 px-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-faint">{{ group }}</div>
        <RouterLink
          v-for="item in navItems.filter((entry) => entry.group === group)"
          :key="item.to"
          :to="item.to"
          :aria-current="active(item.to) ? 'page' : undefined"
          class="mb-0.5 flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-[13px] transition-colors"
          :class="active(item.to) ? 'bg-accent-soft font-semibold text-accent' : 'text-muted hover:bg-surface-3 hover:text-ink'"
        >
          <component :is="item.icon" :size="16" :stroke-width="active(item.to) ? 2.2 : 1.8" class="shrink-0" />
          <span class="flex-1 truncate">{{ item.label }}</span>
          <span
            v-if="badgeValue(item.badge) > 0"
            class="tnum rounded-full px-1.5 py-0.5 text-[10px] font-semibold"
            :class="active(item.to) ? 'bg-accent text-accent-ink' : 'bg-surface-3 text-faint'"
          >
            {{ formatNumber(badgeValue(item.badge)) }}
          </span>
        </RouterLink>
      </div>
    </nav>

    <div class="border-t border-line p-3">
      <div class="rounded-lg border border-line bg-surface-2 p-3">
        <div class="flex items-center justify-between gap-2 text-[11px] text-muted">
          <span>就绪出口</span>
          <span class="tnum font-semibold text-ink">{{ ready }} / {{ target }}</span>
        </div>
        <ProgressBar :value="percent" size="sm" label="出口就绪进度" class="mt-2" />
        <div class="mt-2 flex items-center justify-between text-[10px] text-faint">
          <span>{{ formatNumber(candidates) }} 个候选</span>
          <span>{{ formatVersion(version) }}</span>
        </div>
      </div>
    </div>
  </aside>
</template>
