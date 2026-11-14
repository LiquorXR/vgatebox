<script setup lang="ts">
import { RouterLink, useRoute } from "vue-router";
import { navItems } from "@/lib/nav";

const route = useRoute();

function active(to: string) {
  return to === "/" ? route.path === "/" : route.path.startsWith(to);
}
</script>

<template>
  <nav
    class="fixed inset-x-0 bottom-0 z-30 border-t border-line bg-surface/95 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden"
    aria-label="移动端主导航"
  >
    <div class="grid grid-cols-6">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        :aria-current="active(item.to) ? 'page' : undefined"
        class="flex h-14 flex-col items-center justify-center gap-1 text-[10px]"
        :class="active(item.to) ? 'font-semibold text-accent' : 'text-muted'"
      >
        <component :is="item.icon" :size="18" :stroke-width="active(item.to) ? 2.2 : 1.8" />
        <span>{{ item.label }}</span>
      </RouterLink>
    </div>
  </nav>
</template>
