<script setup lang="ts">
import { useQueryClient } from "@tanstack/vue-query";
import { reactive, ref, watch } from "vue";
import { api, post } from "@/api/client";
import { keys } from "@/api/queries";
import type { AutomationView } from "@/api/types";
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

const queryClient = useQueryClient();
const loading = ref(false);
const saving = ref(false);
const form = reactive({ refresh_enabled: true, refresh_minutes: 60 });

async function load() {
  loading.value = true;
  try {
    const value = await api<AutomationView>("api/console/automation");
    Object.assign(form, value.settings);
  } catch (error) {
    toastError(error, "读取周期任务失败");
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
  const refresh = Number(form.refresh_minutes);
  if (!Number.isSafeInteger(refresh) || refresh < 30 || refresh > 525600) {
    toast("自动刷新间隔须为 30–525600 分钟", { tone: "error" });
    return;
  }
  saving.value = true;
  try {
    await post("api/console/automation", {
      refresh_enabled: form.refresh_enabled,
      refresh_minutes: refresh,
    });
    emit("update:open", false);
    toast("周期任务已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  } catch (error) {
    toastError(error, "保存周期任务失败");
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <DialogShell
    :open="open"
    title="周期任务"
    description="自动刷新最低 30 分钟；每次刷新成功后自动全量检测一次，检测结果 30 分钟有效。获取和检测由本 VPS 完成。"
    size="md"
    @update:open="emit('update:open', $event)"
  >
    <form class="space-y-4" @submit.prevent="save">
      <div class="rounded-lg border border-line p-3">
        <div class="flex items-center justify-between gap-3">
          <div><div class="text-sm font-medium">自动刷新候选清单</div><div class="text-[11px] text-muted">最低 30 分钟；失败保留旧清单，成功后自动全量检测。</div></div>
          <ToggleSwitch v-model="form.refresh_enabled" label="自动刷新候选清单" />
        </div>
        <label class="mt-3 block">
          <span class="mb-1 block text-[11px] text-faint">刷新间隔（分钟）</span>
          <input v-model.number="form.refresh_minutes" name="refresh-minutes" type="number" min="30" max="525600" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
      </div>
      <div class="flex justify-end gap-2 pt-1">
        <UiButton variant="ghost" @click="emit('update:open', false)">取消</UiButton>
        <UiButton type="submit" variant="primary" :loading="saving || loading">保存</UiButton>
      </div>
    </form>
  </DialogShell>
</template>
