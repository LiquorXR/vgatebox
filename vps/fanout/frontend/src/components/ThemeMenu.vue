<script setup lang="ts">
import { Check, Monitor, Moon, Sun } from "@lucide/vue";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "reka-ui";
import UiButton from "./UiButton.vue";
import { setTheme, themeChoice, type ThemeChoice } from "@/lib/theme";

const options: { value: ThemeChoice; label: string; icon: typeof Sun }[] = [
  { value: "auto", label: "跟随系统", icon: Monitor },
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
];

function choose(value: ThemeChoice) {
  setTheme(value);
}
</script>

<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child>
      <UiButton variant="ghost" size="sm" aria-label="切换外观">
        <Moon v-if="themeChoice === 'dark'" :size="16" />
        <Sun v-else-if="themeChoice === 'light'" :size="16" />
        <Monitor v-else :size="16" />
      </UiButton>
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent
        :side-offset="6"
        align="end"
        class="z-[70] min-w-36 rounded-lg border border-line bg-surface p-1 shadow-lg"
      >
        <DropdownMenuItem
          v-for="option in options"
          :key="option.value"
          class="flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-2 text-xs text-ink outline-none hover:bg-surface-3 focus:bg-surface-3"
          @select="choose(option.value)"
        >
          <component :is="option.icon" :size="14" class="text-muted" />
          <span class="flex-1">{{ option.label }}</span>
          <Check v-if="themeChoice === option.value" :size="13" class="text-accent" />
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
