<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    modelValue: boolean;
    disabled?: boolean;
    label: string;
  }>(),
  { disabled: false },
);

const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();

function toggle() {
  if (props.disabled) return;
  emit("update:modelValue", !props.modelValue);
}
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition-colors disabled:opacity-50"
    :class="modelValue ? 'border-accent bg-accent' : 'border-line-strong bg-surface-3'"
    @click="toggle"
  >
    <span
      class="inline-block size-4 rounded-full bg-white shadow-sm transition-transform"
      :class="modelValue ? 'translate-x-[25px]' : 'translate-x-[3px]'"
    />
  </button>
</template>
