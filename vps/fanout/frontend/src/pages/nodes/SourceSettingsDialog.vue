<script setup lang="ts">
import { reactive, ref, watch } from "vue";
import { api, post } from "@/api/client";
import DialogShell from "@/components/DialogShell.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  open: boolean;
}>();

const emit = defineEmits<{
  "update:open": [boolean];
}>();

const loading = ref(false);
const saving = ref(false);
const form = reactive({ mirror_enabled: true, source_timeout_seconds: 10, mirror_max_age_hours: 24 });

async function load() {
  loading.value = true;
  try {
    const value = await api<typeof form>("api/console/source-settings");
    Object.assign(form, value);
  } catch (error) {
    toastError(error, "读取来源设置失败");
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) void load();
  },
  { immediate: true },
);

async function save() {
  saving.value = true;
  try {
    await post("api/console/source-settings", {
      mirror_enabled: form.mirror_enabled,
      source_timeout_seconds: Number(form.source_timeout_seconds),
      mirror_max_age_hours: Number(form.mirror_max_age_hours),
    });
    emit("update:open", false);
    toast("来源设置已保存，下次刷新生效", { tone: "success" });
  } catch (error) {
    toastError(error, "保存来源设置失败");
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <DialogShell
    :open="open"
    title="候选来源设置"
    description="所有清单获取和节点检测都由本 VPS 执行。"
    size="md"
    @update:open="emit('update:open', $event)"
  >
    <form class="space-y-4" @submit.prevent="save">
      <label class="flex items-start justify-between gap-4 rounded-lg border border-line p-3">
        <span>
          <span class="block text-sm font-medium text-ink">允许第三方镜像后备</span>
          <span class="mt-0.5 block text-[11px] leading-relaxed text-muted">仅在当前来源失败时请求镜像原始 CSV；不采用镜像的检测结论。</span>
        </span>
        <ToggleSwitch v-model="form.mirror_enabled" label="允许第三方镜像后备" />
      </label>
      <label class="block">
        <span class="mb-1 block text-xs font-medium">每次来源请求最长等待（秒）</span>
        <input v-model.number="form.source_timeout_seconds" name="source-timeout" type="number" min="2" max="20" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
      </label>
      <label class="block">
        <span class="mb-1 block text-xs font-medium">镜像声明抓取时间的最大年龄（小时）</span>
        <input v-model.number="form.mirror_max_age_hours" name="mirror-max-age" type="number" min="1" max="168" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        <span class="mt-1 block text-[11px] text-faint">默认 24 小时；超过 3 小时会提示较旧。校验失败时保留旧清单。</span>
      </label>
      <div class="flex justify-end gap-2 pt-1">
        <UiButton variant="ghost" @click="emit('update:open', false)">取消</UiButton>
        <UiButton type="submit" variant="primary" :loading="saving || loading">保存</UiButton>
      </div>
    </form>
  </DialogShell>
</template>
