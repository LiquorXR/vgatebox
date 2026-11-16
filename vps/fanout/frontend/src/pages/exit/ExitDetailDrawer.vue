<script setup lang="ts">
import { ArrowLeftRight, Pause, Play, RefreshCw, RotateCcw, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { post, queryString } from "@/api/client";
import { keys } from "@/api/queries";
import type { Exit, ExitInbound, SlotPolicy } from "@/api/types";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import DialogShell from "@/components/DialogShell.vue";
import LatencyTrend from "@/components/LatencyTrend.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { lastExitText as lastExitTextAt, retryText as retryTextAt, useNow } from "@/lib/exit-time";
import { countryName, formatDateTime, formatLatency, pad2 } from "@/lib/format";
import { clonePolicy, defaultPolicy } from "@/lib/policy";
import { toast, toastError } from "@/lib/toast";
import {
  exitName,
  exitStatusLabel,
  exitStatusTone,
  exitSubLabel,
  ipTypeLabels,
  phaseLabels,
} from "@/lib/status";

const props = defineProps<{
  open: boolean;
  exit: Exit | null;
  countryOptions: [string, string][];
  checkRunning: boolean;
}>();

const emit = defineEmits<{
  "update:open": [boolean];
}>();

const queryClient = useQueryClient();

const nameEdits = reactive<Record<number, { mode: string; remark: string }>>({});
const policyForm = reactive<SlotPolicy>(defaultPolicy());
const swapConfirmOpen = ref(false);
const deleteExitConfirmOpen = ref(false);
const deleteCleanupArchive = ref(false);
const selectedManagedInbounds = computed(() =>
  (props.exit?.inbounds || []).filter((inbound) => inbound.managed_slot),
);
const countryOptionsWithCurrent = computed(() => {
  const options = [...props.countryOptions];
  const current = policyForm.country;
  if (current && !options.some(([code]) => code === current)) {
    options.push([current, countryName(current)]);
  }
  return options.sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
});

function setOpen(value: boolean) {
  emit("update:open", value);
}

watch(
  () => props.exit?.slot,
  () => {
    for (const key of Object.keys(nameEdits)) delete nameEdits[Number(key)];
    for (const inbound of props.exit?.inbounds || []) {
      nameEdits[inbound.id] = {
        mode: inbound.name_mode === "custom" ? "custom" : "auto",
        remark: inbound.remark || "",
      };
    }
    Object.assign(policyForm, props.exit?.policy ? clonePolicy(props.exit.policy) : defaultPolicy());
  },
  { immediate: true },
);

const nowMs = useNow();
const retryText = (exit: Exit) => retryTextAt(exit, nowMs.value);
const lastExitText = (exit: Exit) => lastExitTextAt(exit, nowMs.value);

const checkMutation = useMutation({
  mutationFn: (slot: number) => post("api/console/check", { slots: [slot], priority: true }),
  onSuccess: () => {
    toast("已提交检测");
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "提交检测失败"),
});

const pauseMutation = useMutation({
  mutationFn: ({ slot, resume }: { slot: number; resume: boolean }) =>
    post(resume ? `api/slot/resume${queryString({ slot })}` : `api/stop${queryString({ slot })}`, {}),
  onSuccess: (_, variables) => {
    toast(variables.resume ? "正在恢复出口" : "已暂停出口", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "操作失败"),
});

const swapMutation = useMutation({
  mutationFn: (slot: number) => post(`api/swap${queryString({ slot })}`, {}),
  onSuccess: () => {
    swapConfirmOpen.value = false;
    toast("正在更换节点；出口编号和端口保持不变", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "更换节点失败"),
});

const deleteExitMutation = useMutation({
  mutationFn: ({ slot, cleanup }: { slot: number; cleanup: boolean }) =>
    post<{ ok: boolean; cleanup_error?: string }>("api/console/slot/delete", {
      slot,
      cleanup_archive: cleanup,
    }),
  onSuccess: (value) => {
    deleteExitConfirmOpen.value = false;
    setOpen(false);
    if (value.cleanup_error) {
      toast(`出口已删除，但归档入口清理失败：${value.cleanup_error}`, { tone: "error" });
    } else {
      toast("出口已删除，目标数量已减少 1", { tone: "success" });
    }
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
    void queryClient.invalidateQueries({ queryKey: keys.inbounds });
  },
  onError: (error) => toastError(error, "删除出口失败"),
});

const nameMutation = useMutation({
  mutationFn: ({ inbound, mode, remark }: { inbound: ExitInbound; mode: string; remark: string }) => {
    if (mode === "custom" && !remark.trim()) throw new Error("请输入自定义名称");
    return post(
      `api/panel/inbound/update${queryString({
        id: inbound.id,
        name_mode: mode,
        ...(mode === "custom" ? { remark: remark.trim() } : {}),
      })}`,
      {},
    );
  },
  onSuccess: () => {
    toast("订阅名称已更新", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "保存名称失败"),
});

const policyMutation = useMutation({
  mutationFn: (slot: number) => post(`api/slot/policy${queryString({ slot })}`, clonePolicy(policyForm)),
  onSuccess: () => {
    toast("策略已保存，出口正在按新策略重新验活", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "保存策略失败"),
});

const rollbackMutation = useMutation({
  mutationFn: (slot: number) => post(`api/slot/policy/rollback${queryString({ slot })}`, {}),
  onSuccess: () => {
    toast("已回滚到上一份策略", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "回滚失败"),
});

function saveName(inbound: ExitInbound) {
  const edit = nameEdits[inbound.id];
  if (!edit) return;
  nameMutation.mutate({ inbound, mode: edit.mode, remark: edit.remark });
}

function openDeleteExit() {
  deleteCleanupArchive.value = false;
  deleteExitConfirmOpen.value = true;
}

function toggleType(type: string, checked: boolean) {
  const values = new Set(policyForm.allowed_ip_types || []);
  if (checked) values.add(type);
  else values.delete(type);
  policyForm.allowed_ip_types = [...values];
}

function listText(value: string[] | undefined): string {
  return (value || []).join(", ");
}

function parseList(value: string): string[] {
  return value
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function getPolicyList(key: string): string {
  return listText((policyForm as unknown as Record<string, string[]>)[key]);
}

function setPolicyList(key: string, value: string): void {
  (policyForm as unknown as Record<string, string[]>)[key] = parseList(value);
}
</script>

<template>
  <DialogShell
    :open="open"
    :title="exit ? exitName(exit) : '出口详情'"
    :description="exit ? exitSubLabel(exit) : undefined"
    variant="drawer"
    @update:open="setOpen"
  >
    <div v-if="!exit" class="text-sm text-muted">出口不存在，可能已被移除。</div>
    <div v-else class="space-y-5">
      <div class="flex flex-wrap items-center gap-2">
        <StatusBadge :label="exitStatusLabel(exit.status)" :tone="exitStatusTone(exit.status)" :pulse="exit.status === 'starting'" />
        <span v-if="exit.phase" class="text-[11px] text-faint">{{ phaseLabels[exit.phase] || exit.phase }}<span v-if="exit.phase_elapsed_s"> · {{ exit.phase_elapsed_s }}s</span></span>
        <span v-if="exit.pending_swap" class="text-[11px] text-warn">排队换节点中，当前连接完成后执行</span>
        <span v-if="retryText(exit)" class="text-[11px] text-muted">{{ retryText(exit) }}</span>
        <span v-if="exit.err" class="text-[11px] text-bad">{{ exit.err }}</span>
      </div>

      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
        <div><dt class="text-faint">出口编号</dt><dd class="tnum mt-0.5 font-medium">{{ pad2(exit.slot) }}</dd></div>
        <div><dt class="text-faint">延迟</dt><dd class="mt-0.5 flex items-center gap-1 font-medium"><span class="tnum">{{ formatLatency(exit.latency_ms) }}</span><LatencyTrend :trend="exit.latency_trend" /></dd></div>
        <div><dt class="text-faint">实际出口</dt><dd class="mt-0.5 truncate font-mono text-[11px]">{{ exit.exit_ip || "—" }}<span v-if="lastExitText(exit)" class="mt-0.5 block truncate font-sans text-[10px] text-faint">{{ lastExitText(exit) }}</span></dd></div>
        <div><dt class="text-faint">来源地区</dt><dd class="mt-0.5">{{ countryName(exit.region) }}（{{ exit.region || "??" }}）</dd></div>
        <div><dt class="text-faint">启动于</dt><dd class="mt-0.5">{{ formatDateTime(exit.since) }}</dd></div>
        <div><dt class="text-faint">接入端口</dt><dd class="tnum mt-0.5">{{ exit.port || "—" }}</dd></div>
      </dl>

      <div class="flex flex-wrap gap-2">
        <UiButton size="sm" :loading="checkMutation.isPending.value" :disabled="checkRunning" @click="checkMutation.mutate(exit.slot)">
          <RefreshCw :size="14" />立即检测
        </UiButton>
        <UiButton size="sm" :loading="swapMutation.isPending.value" @click="swapConfirmOpen = true">
          <ArrowLeftRight :size="14" />更换 IP
        </UiButton>
        <UiButton
          v-if="exit.status === 'stopped'"
          size="sm"
          :loading="pauseMutation.isPending.value"
          @click="pauseMutation.mutate({ slot: exit.slot, resume: true })"
        >
          <Play :size="14" />恢复
        </UiButton>
        <UiButton
          v-else
          size="sm"
          :loading="pauseMutation.isPending.value"
          @click="pauseMutation.mutate({ slot: exit.slot, resume: false })"
        >
          <Pause :size="14" />暂停
        </UiButton>
        <UiButton
          v-if="exit.can_rollback_policy"
          size="sm"
          :loading="rollbackMutation.isPending.value"
          @click="rollbackMutation.mutate(exit.slot)"
        >
          <RotateCcw :size="14" />回滚策略
        </UiButton>
        <UiButton size="sm" variant="danger" @click="openDeleteExit">
          <Trash2 :size="14" />删除出口
        </UiButton>
      </div>

      <section>
        <h3 class="mb-2 text-xs font-semibold text-ink">订阅名称</h3>
        <div v-if="exit.inbounds?.length" class="space-y-2">
          <div v-for="inbound in exit.inbounds" :key="inbound.id" class="rounded-lg border border-line p-3">
            <div class="mb-2 flex items-center justify-between gap-2 text-[11px] text-faint">
              <span class="font-mono">{{ inbound.protocol.toUpperCase() }} · {{ inbound.port }}</span>
              <span>{{ inbound.managed_slot ? "自动入口" : "手动入口" }} · {{ inbound.enable ? "已启用" : "已停用" }}</span>
            </div>
            <div v-if="nameEdits[inbound.id]" class="flex flex-wrap gap-2">
              <select v-model="nameEdits[inbound.id].mode" name="inbound-name-mode" class="h-8 rounded-md border border-line-strong bg-surface px-2 text-xs" :aria-label="`名称模式 ${inbound.port}`">
                <option value="auto">国家 + 编号</option>
                <option value="custom">自定义名称</option>
              </select>
              <input
                v-if="nameEdits[inbound.id].mode === 'custom'"
                v-model="nameEdits[inbound.id].remark"
                name="inbound-name"
                class="h-8 min-w-0 flex-1 rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent"
                placeholder="客户端显示的名称"
                :aria-label="`自定义名称 ${inbound.port}`"
              />
              <UiButton size="sm" :loading="nameMutation.isPending.value" @click="saveName(inbound)">保存</UiButton>
            </div>
          </div>
        </div>
        <p v-else class="text-xs text-faint">等待自动绑定入站。</p>
      </section>

      <section>
        <h3 class="mb-2 text-xs font-semibold text-ink">选择策略</h3>
        <form class="space-y-3" @submit.prevent="policyMutation.mutate(exit.slot)">
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">指定国家</span>
              <select v-model="policyForm.country" name="policy-country" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent">
                <option value="">留空自动</option>
                <option v-for="[code, label] in countryOptionsWithCurrent" :key="code" :value="code">{{ label }}（{{ code }}）</option>
              </select>
            </label>
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">延迟上限（毫秒，0 不限）</span>
              <input v-model.number="policyForm.maximum_latency_ms" name="policy-latency" type="number" inputmode="numeric" min="0" max="120000" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent" />
            </label>
          </div>
          <div>
            <span class="mb-1 block text-[11px] text-faint">允许的 IP 类型</span>
            <div class="grid grid-cols-2 gap-1.5">
              <label v-for="(label, type) in ipTypeLabels" :key="type" class="flex items-center gap-2 text-xs text-muted">
                <input type="checkbox" name="policy-ip-type" :value="type" :checked="policyForm.allowed_ip_types?.includes(type)" class="accent-accent" @change="toggleType(type, ($event.target as HTMLInputElement).checked)" />
                {{ label }}
              </label>
            </div>
          </div>
          <details class="rounded-md border border-line px-3 py-2">
            <summary class="cursor-pointer text-xs text-muted">ISP / ASN 限制（可选）</summary>
            <div class="mt-2 grid gap-2 sm:grid-cols-2">
              <label v-for="field in [
                { key: 'allowed_isp', label: '允许 ISP' },
                { key: 'blocked_isp', label: '排除 ISP' },
                { key: 'allowed_asn', label: '允许 ASN' },
                { key: 'blocked_asn', label: '排除 ASN' },
              ]" :key="field.key" class="block">
                <span class="mb-0.5 block text-[10px] text-faint">{{ field.label }}（逗号分隔）</span>
                <input
                  :value="getPolicyList(field.key)"
                  :name="`policy-${field.key}`"
                  class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 font-mono text-[11px] outline-none focus:border-accent"
                  @input="setPolicyList(field.key, ($event.target as HTMLInputElement).value)"
                />
              </label>
            </div>
          </details>
          <UiButton type="submit" size="sm" variant="primary" :loading="policyMutation.isPending.value">保存策略</UiButton>
        </form>
      </section>
    </div>
  </DialogShell>

  <ConfirmDialog
    :open="deleteExitConfirmOpen"
    title="删除出口"
    description="删除后目标数量会减少 1，其他出口编号和端口保持不变。该出口的客户端连接和订阅入口会立即失效。自动入口默认归档保留，重建同一编号时可复用。"
    confirm-label="删除出口"
    :busy="deleteExitMutation.isPending.value"
    @update:open="deleteExitConfirmOpen = $event"
    @confirm="exit && deleteExitMutation.mutate({ slot: exit.slot, cleanup: deleteCleanupArchive })"
  >
    <label
      v-if="selectedManagedInbounds.length"
      class="flex items-start gap-2 rounded-lg border border-line p-3 text-xs text-muted"
    >
      <input v-model="deleteCleanupArchive" type="checkbox" name="cleanup-archive" class="mt-0.5 accent-accent" />
      <span>
        同时清理归档入口
        <span class="mt-0.5 block text-[11px] text-faint">勾选后会删除该出口的自动入口和客户端凭据，无法恢复；不勾选则保留归档，之后重建同一编号时复用。</span>
      </span>
    </label>
  </ConfirmDialog>

  <ConfirmDialog
    :open="swapConfirmOpen"
    title="更换出口 IP"
    description="将断开当前节点并重新挑选一个候选；出口编号、端口和订阅入口保持不变。正在使用该出口的客户端会短暂中断。"
    confirm-label="更换 IP"
    :busy="swapMutation.isPending.value"
    @update:open="swapConfirmOpen = $event"
    @confirm="exit && swapMutation.mutate(exit.slot)"
  />
</template>
