<script setup lang="ts">
import { RouterView } from "vue-router";
import MobileNav from "./MobileNav.vue";
import SideNav from "./SideNav.vue";
import TopBar from "./TopBar.vue";
</script>

<template>
  <div class="min-h-dvh bg-canvas text-ink">
    <SideNav />
    <div class="lg:pl-56">
      <TopBar />
      <main class="mx-auto w-full max-w-[1500px] px-4 pb-24 pt-5 sm:px-6 lg:px-8 lg:pb-10">
        <RouterView v-slot="{ Component }">
          <component :is="Component" />
        </RouterView>
      </main>
    </div>
    <MobileNav />
  </div>
</template>
