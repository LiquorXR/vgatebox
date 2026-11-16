<script setup lang="ts">
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, ref, watch } from "vue";
import { post } from "@/api/client";
import { keys } from "@/api/queries";
import type { ConsoleSummary, ExitPlan, SlotPolicy } from "@/api/types";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import DialogShell from "@/components/DialogShell.vue";
import UiButton from "@/components/UiButton.vue";
import { countryName } from "@/lib/format";
import { initialTarget } from "@/lib/plan";
import { clonePolicy, defaultPolicy } from "@/lib/policy";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  summary: ConsoleSummary | null | undefined;
  allocated: number;
  countryOptions: [string, string][];
}>();

const open = defineModel<boolean>("open", { required: true });
const queryClient = useQueryClient();

const targetCount = ref(6);
const targetCountry = ref("");
const targetServer = ref("www.tesla.com");
const confirmOpen = ref(false);
const pendingSave = ref<(() => Promise<void>) | null>(null);

watch(
  open,
  (value) => {
    if (!value) {
      confirmOpen.value = false;
      return;
    }
    const summary = props.summary;
    if (!summary) return;
    targetCount.value = initialTarget(summary);
    targetCountry.value = summary.plan?.policy?.country || "";
    targetServer.value = summary.plan?.server_name || summary.default_server_name || "www.tesla.com";
  },
  { immediate: true },
);

const targetMutation = useMutation({
  mutationFn: async () => {
    const count = Math.max(0, Math.trunc(Number(targetCount.value)));
    if (!Number.isFinite(count) || count < 0 || count > (props.summary?.capacity || 0)) {
      throw new Error(`出口数量须为 0–${props.summary?.capacity || 0}`);
    }
    const base = props.summary?.plan?.policy;
    const policy: SlotPolicy = base ? clonePolicy(base) : defaultPolicy();
    policy.country = targetCountry.value.trim().toUpperCase();
    const serverName = targetServer.value.trim().toLowerCase();
    if (!serverName || !serverName.includes(".")) throw new Error("REALITY 目标须为有效域名");
    const body: ExitPlan = { count, policy, server_name: serverName };
    return post("api/console/target", body);
  },
  onSuccess: () => {
    toast("目标数量已保存，正在按策略连接", { tone: "success" });
    open.value = false;
    confirmOpen.value = false;
    void queryClient.invalidateQueries({ queryKey: keys.console });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "保存出口数量失败"),
});

function submitTarget() {
  const count = Math.max(0, Math.trunc(Number(targetCount.value)));
  if (count < props.allocated) {
    pendingSave.value = async () => {
      await targetMutation.mutateAsync();
    };
    confirmOpen.value = true;
    return;
  }
  targetMutation.mutate();
}

const targetRegionOptions = computed(() => {
  const stats = props.summary?.regions || [];
  const options = stats.length
    ? stats.map((region) => ({
        code: region.code,
        name: region.name || countryName(region.code),
        available: region.available,
        best_ping: region.best_ping,
        best_speed_mbps: region.best_speed_mbps,
      }))
    : props.countryOptions.map(([code, name]) => ({
        code,
        name,
        available: 0,
        best_ping: 0,
        best_speed_mbps: 0,
      }));
  if (targetCountry.value && !options.some((region) => region.code === targetCountry.value)) {
    options.push({
      code: targetCountry.value,
      name: countryName(targetCountry.value),
      available: 0,
      best_ping: 0,
      best_speed_mbps: 0,
    });
  }
  return options;
});

function regionOptionLabel(region: {
  name: string;
  code: string;
  available: number;
  best_ping: number;
  best_speed_mbps: number;
}): string {
  let label = `${region.name}（${region.code}）`;
  if (region.available > 0) label += ` · ${region.available} 个空闲`;
  if (region.best_ping > 0) label += ` · ${region.best_ping}ms`;
  if (region.best_speed_mbps > 0) label += ` · ${region.best_speed_mbps}Mbps`;
  return label;
}
</script>

<template>
  <DialogShell
    :open="open"
    title="调整 VPN Gate 数量"
    description="按服务器资源和上游供给设置目标；候选不足时会保留策略等待。"
    size="md"
    @update:open="open = $event"
  >
    <form class="space-y-4" @submit.prevent="submitTarget">
      <label class="block">
        <span class="mb-1 block text-xs font-medium text-ink">目标数量</span>
        <input
          v-model.number="targetCount"
          name="target-count"
          type="number"
          inputmode="numeric"
          min="0"
          :max="summary?.capacity || 0"
          required
          class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent"
        />
        <span class="mt-1 block text-[11px] text-faint">当前已分配 {{ allocated }} 个；设为 0 会移除全部活动出口。</span>
      </label>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block">
          <span class="mb-1 block text-xs font-medium text-ink">新增出口国家</span>
          <select
            v-model="targetCountry"
            name="target-country"
            class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent"
          >
            <option value="">自动选择</option>
            <option v-for="region in targetRegionOptions" :key="region.code" :value="region.code">
              {{ regionOptionLabel(region) }}
            </option>
          </select>
          <span class="mt-1 block text-[11px] text-faint">空闲数量是当前候选库存，不代表已通过检测。</span>
        </label>
        <label class="block">
          <span class="mb-1 block text-xs font-medium text-ink">REALITY 目标域名</span>
          <input
            v-model="targetServer"
            name="target-server"
            class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent"
          />
          <span class="mt-1 block text-[11px] text-faint">默认 {{ summary?.default_server_name || "www.tesla.com" }}。</span>
        </label>
      </div>
      <div
        v-if="targetCount < allocated"
        class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-xs leading-relaxed text-warn"
      >
        目标数量低于当前分配数，将从编号最大的出口开始移除，相关连接会中断。
      </div>
      <div class="flex justify-end gap-2 pt-1">
        <UiButton variant="ghost" @click="open = false">取消</UiButton>
        <UiButton type="submit" variant="primary" :loading="targetMutation.isPending.value">保存并应用</UiButton>
      </div>
    </form>
  </DialogShell>

  <ConfirmDialog
    :open="confirmOpen"
    title="确认减少出口数量"
    :description="`将从编号最大的出口开始移除，目标数量为 ${targetCount} 个；正在使用的连接会中断。`"
    confirm-label="确认移除"
    :busy="targetMutation.isPending.value"
    @update:open="confirmOpen = $event"
    @confirm="pendingSave?.()"
  />
</template>
