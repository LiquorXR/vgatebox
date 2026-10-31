<script setup lang="ts">
import { Check, Copy } from "@lucide/vue";
import { onBeforeUnmount, ref } from "vue";
import { copyText } from "@/lib/clipboard";
import { toast, toastError } from "@/lib/toast";
import UiButton from "./UiButton.vue";

const props = withDefaults(
  defineProps<{
    text: string;
    label?: string;
    copiedLabel?: string;
    variant?: "primary" | "secondary" | "ghost";
    size?: "sm" | "md";
    disabled?: boolean;
  }>(),
  { label: "复制", copiedLabel: "已复制", variant: "secondary", size: "md", disabled: false },
);

const copied = ref(false);
let timer: number | undefined;

async function run() {
  try {
    await copyText(props.text);
    copied.value = true;
    toast(props.copiedLabel || "已复制", { tone: "success", duration: 1800 });
    window.clearTimeout(timer);
    timer = window.setTimeout(() => (copied.value = false), 1600);
  } catch (error) {
    toastError(error, "复制失败");
  }
}

onBeforeUnmount(() => window.clearTimeout(timer));
</script>

<template>
  <UiButton :variant="variant" :size="size" :disabled="disabled || !text" @click="run">
    <Check v-if="copied" :size="size === 'sm' ? 13 : 15" />
    <Copy v-else :size="size === 'sm' ? 13 : 15" />
    <span>{{ copied ? copiedLabel : label }}</span>
  </UiButton>
</template>
