<script setup lang="ts">
import { Copy, Download, Globe2, Search, ShieldCheck } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { apiUrl, post, queryString } from "@/api/client";
import { keys } from "@/api/queries";
import type { SstpView } from "@/api/types";
import EmptyState from "@/components/EmptyState.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { copyText } from "@/lib/clipboard";
import { downloadText } from "@/lib/download";
import { countryName, formatDateTime, formatLatency, truncate } from "@/lib/format";
import { checkStatusLabel, checkStatusTone, errorLabel } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  data: SstpView | null | undefined;
  loading: boolean;
}>();

const queryClient = useQueryClient();
const sstpData = computed(() => props.data);
const sstpLoading = computed(() => props.loading);

const sstpSearch = ref("");
const sstpSelected = ref<Set<string>>(new Set());
const sstpSettings = reactive({ parallelism: 2, interval_minutes: 0, valid_minutes: 60, timeout_seconds: 90 });

watch(
  () => sstpData.value?.settings,
  (value) => {
    if (value) Object.assign(sstpSettings, value);
  },
  { immediate: true },
);

const sstpRows = computed(() => {
  const term = sstpSearch.value.trim().toLowerCase();
  const rows = sstpData.value?.nodes || [];
  if (!term) return rows;
  return rows.filter((row) =>
    [row.hostname, row.server_ip, row.source_country, row.result.actual_ip, row.result.country]
      .join(" ")
      .toLowerCase()
      .includes(term),
  );
});

const sstpCheckRunning = computed(() => sstpData.value?.check?.status === "running");
const sstpPercent = computed(() => {
  const check = sstpData.value?.check;
  if (!check?.total) return 0;
  return (check.done / check.total) * 100;
});

function toggleSstp(host: string, checked: boolean) {
  const next = new Set(sstpSelected.value);
  if (checked) next.add(host);
  else next.delete(host);
  sstpSelected.value = next;
}

const sstpCheckMutation = useMutation({
  mutationFn: (body: Record<string, unknown>) => post("api/resources/sstp/check", body),
  onSuccess: () => {
    toast("SSTP 检测任务已提交");
    void queryClient.invalidateQueries({ queryKey: keys.sstp });
  },
  onError: (error) => toastError(error, "提交 SSTP 检测失败"),
});

const sstpSettingsMutation = useMutation({
  mutationFn: () => post("api/resources/sstp/settings", { ...sstpSettings }),
  onSuccess: () => {
    toast("SSTP 设置已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.sstp });
  },
  onError: (error) => toastError(error, "保存 SSTP 设置失败"),
});

const sstpCancelMutation = useMutation({
  mutationFn: () => post("api/resources/sstp/cancel", {}),
  onSuccess: () => toast("正在取消 SSTP 检测"),
  onError: (error) => toastError(error, "取消失败"),
});

async function sstpFetch(hosts: string[] | undefined, format: "json" | "text"): Promise<{ text: string; ok: boolean }> {
  const response = await fetch(
    apiUrl(`api/resources/sstp/export${queryString({ format, hosts: hosts?.length ? hosts.join(",") : undefined })}`),
    { credentials: "same-origin", cache: "no-store" },
  );
  const text = await response.text();
  if (!response.ok) throw new Error(text.trim() || `导出失败（${response.status}）`);
  return { text, ok: true };
}

async function exportSstp(format: "json" | "text") {
  try {
    const hosts = [...sstpSelected.value];
    const result = await sstpFetch(hosts.length ? hosts : undefined, format);
    if (format === "json") downloadText("vgatebox-sstp.json", result.text, "application/json;charset=utf-8");
    else await copyText(result.text);
    toast(format === "json" ? "SSTP 结果已下载" : "SSTP 连接参数已复制", { tone: "success" });
  } catch (error) {
    toastError(error, "导出 SSTP 失败");
  }
}
</script>

<template>
  <div class="space-y-4">
    <Panel title="检测与更新" description="自动检测与手动任务共用本页并发额度；SSTP 不占用现有出口数量。">
      <form class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" @submit.prevent="sstpSettingsMutation.mutate()">
        <label class="block">
          <span class="mb-1 block text-[11px] text-faint">检测与建连并发</span>
          <input v-model.number="sstpSettings.parallelism" name="sstp-parallelism" type="number" inputmode="numeric" min="1" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <label class="block">
          <span class="mb-1 block text-[11px] text-faint">自动检测间隔（分钟，0 关闭）</span>
          <input v-model.number="sstpSettings.interval_minutes" name="sstp-interval" type="number" inputmode="numeric" min="0" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <label class="block">
          <span class="mb-1 block text-[11px] text-faint">结果有效期（分钟）</span>
          <input v-model.number="sstpSettings.valid_minutes" name="sstp-valid" type="number" inputmode="numeric" min="1" max="10080" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <label class="block">
          <span class="mb-1 block text-[11px] text-faint">单节点超时（秒）</span>
          <input v-model.number="sstpSettings.timeout_seconds" name="sstp-timeout" type="number" inputmode="numeric" min="10" max="3600" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <div class="sm:col-span-2 lg:col-span-4">
          <UiButton type="submit" size="sm" :loading="sstpSettingsMutation.isPending.value">保存设置</UiButton>
        </div>
      </form>
    </Panel>

    <div v-if="sstpData?.dependency_error" class="rounded-xl border border-bad/25 bg-bad-soft px-4 py-3 text-xs leading-relaxed text-bad">
      SSTP 依赖不可用：{{ sstpData.dependency_error }}
    </div>

    <div v-if="sstpData?.check" class="rounded-xl border border-line bg-surface px-4 py-3">
      <div class="flex items-center justify-between gap-2 text-xs">
        <span class="font-medium">SSTP 检测 · {{ sstpData.check.done }} / {{ sstpData.check.total }}</span>
        <StatusBadge :label="checkStatusLabel(sstpData.check.status)" :tone="checkStatusTone(sstpData.check.status)" size="sm" :pulse="sstpData.check.status === 'running'" />
      </div>
      <ProgressBar :value="sstpPercent" size="sm" class="mt-2" label="SSTP 检测进度" />
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <label class="relative min-w-[200px] flex-1">
        <Search :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-faint" />
        <input v-model="sstpSearch" name="sstp-search" type="search" autocomplete="off" placeholder="搜索节点或 IP" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
      </label>
      <UiButton size="sm" :disabled="!sstpSelected.size" :loading="sstpCheckMutation.isPending.value" @click="sstpCheckMutation.mutate({ hosts: [...sstpSelected] })">
        <ShieldCheck :size="14" />检测勾选
      </UiButton>
      <UiButton size="sm" :loading="sstpCheckMutation.isPending.value" @click="sstpCheckMutation.mutate({ all: true })">检测全部</UiButton>
      <UiButton v-if="sstpCheckRunning" size="sm" variant="ghost" :loading="sstpCancelMutation.isPending.value" @click="sstpCancelMutation.mutate()">取消</UiButton>
      <UiButton size="sm" variant="ghost" @click="exportSstp('json')"><Download :size="14" />导出 JSON</UiButton>
      <UiButton size="sm" variant="ghost" :disabled="!sstpSelected.size" @click="exportSstp('text')"><Copy :size="14" />复制勾选</UiButton>
    </div>

    <Panel flush>
      <div v-if="sstpLoading && !sstpData" class="p-4"><SkeletonBlock :lines="7" /></div>
      <div v-else-if="!sstpRows.length" class="p-4">
        <EmptyState title="还没有 SSTP 候选" description="刷新候选来源后，来源节点声明了 SSTP 端口才会出现在这里。">
          <template #icon><Globe2 :size="26" /></template>
        </EmptyState>
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full min-w-[940px] text-left text-xs">
          <caption class="sr-only">原始 SSTP 候选</caption>
          <thead>
            <tr class="border-b border-line text-[11px] text-faint">
              <th class="w-10 px-4 py-2"><span class="sr-only">选择</span></th>
              <th class="px-2 py-2 font-medium">来源节点</th>
              <th class="px-2 py-2 font-medium">来源地区</th>
              <th class="px-2 py-2 font-medium">候选端口</th>
              <th class="px-2 py-2 font-medium">实际出口</th>
              <th class="px-2 py-2 font-medium">VPS 检测</th>
              <th class="px-2 py-2 font-medium">HTTPS 延迟</th>
              <th class="px-2 py-2 font-medium">检测 / 到期</th>
              <th class="px-4 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in sstpRows" :key="row.hostname" class="border-b border-line last:border-b-0 hover:bg-surface-2">
              <td class="px-4 py-2.5">
                <input type="checkbox" name="sstp-select" :value="row.hostname" :checked="sstpSelected.has(row.hostname)" :aria-label="`选择 ${row.hostname}`" class="accent-accent" @change="toggleSstp(row.hostname, ($event.target as HTMLInputElement).checked)" />
              </td>
              <td class="px-2 py-2.5">
                <div class="truncate font-mono text-[11px]">{{ truncate(row.hostname, 28) }}</div>
                <div class="text-[10px] text-faint">{{ row.server_ip }}</div>
              </td>
              <td class="px-2 py-2.5">{{ countryName(row.source_country) }}（{{ row.source_country }}）</td>
              <td class="px-2 py-2.5 font-mono text-[11px]">{{ row.candidates.map((item) => item.port).join(", ") || "—" }}</td>
              <td class="px-2 py-2.5">
                <div class="font-mono text-[11px]">{{ row.result.actual_ip || "—" }}</div>
                <div class="text-[10px] text-faint">{{ row.result.country ? countryName(row.result.country) : "地区未知" }}</div>
              </td>
              <td class="px-2 py-2.5">
                <StatusBadge :label="checkStatusLabel(row.attempt?.status || row.result.status)" :tone="checkStatusTone(row.attempt?.status || row.result.status)" size="sm" />
                <div v-if="row.attempt?.code" class="mt-1 text-[10px] text-bad">{{ errorLabel(row.attempt.code) }}</div>
              </td>
              <td class="tnum px-2 py-2.5 text-muted">{{ formatLatency(row.result.latency_ms) }}</td>
              <td class="px-2 py-2.5 text-[10px] text-faint">
                <div>{{ formatDateTime(row.result.checked_at) }}</div>
                <div>{{ formatDateTime(row.result.expires_at) }}</div>
              </td>
              <td class="px-4 py-2.5 text-right">
                <UiButton size="sm" variant="ghost" @click="sstpCheckMutation.mutate({ hosts: [row.hostname] })">检测</UiButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Panel>
  </div>
</template>
