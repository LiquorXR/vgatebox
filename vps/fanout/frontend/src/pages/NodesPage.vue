<script setup lang="ts">
import { Database, RefreshCw, Search, Settings2, ShieldCheck, TimerReset, X } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { api, post } from "@/api/client";
import { keys, useConsoleQuery, useNodesQuery } from "@/api/queries";
import type { AutomationView, CheckResult, Node } from "@/api/types";
import DialogShell from "@/components/DialogShell.vue";
import EmptyState from "@/components/EmptyState.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { countryName, formatDateTime, formatLatency, formatMbps, truncate } from "@/lib/format";
import { scheduleLabel, sourceLabel } from "@/lib/automation";
import { checkStatusLabel, checkStatusTone, errorLabel, ipTypeLabel } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const PAGE_SIZE = 50;
const queryClient = useQueryClient();
const { data: summary } = useConsoleQuery();
const { data: view, isLoading, isError, error, refetch } = useNodesQuery(true);

const nodes = computed(() => view.value?.nodes || []);
const search = ref("");
const countryFilter = ref("");
const statusFilter = ref("all");
const page = ref(1);
const selected = ref<Set<string>>(new Set());
const parallelism = ref(2);
const parallelismDirty = ref(false);
const submitting = reactive(new Set<string>());

watch(
  () => summary.value?.check_settings?.parallelism,
  (value) => {
    if (value && !parallelismDirty.value) parallelism.value = value;
  },
  { immediate: true },
);

function effectiveResult(node: Node): CheckResult {
  if (submitting.has(node.hostname)) return { host: node.hostname, status: "submitting" };
  const pending = summary.value?.check?.items?.find(
    (item) => item.host === node.hostname && ["running", "queued", "priority_queued"].includes(item.status),
  );
  if (pending) return pending;
  const result = summary.value?.results?.[node.hostname];
  if (!result) return { host: node.hostname, status: "not_tested" };
  return result.stale ? { ...result, status: "stale" } : result;
}

const countries = computed(() => {
  const map = new Map<string, string>();
  for (const node of nodes.value) {
    if (node.country_code) map.set(node.country_code, node.country || countryName(node.country_code));
  }
  return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
});

const filtered = computed(() => {
  const term = search.value.trim().toLowerCase();
  return nodes.value.filter((node) => {
    if (countryFilter.value && node.country_code !== countryFilter.value) return false;
    if (statusFilter.value !== "all") {
      const result = effectiveResult(node);
      if (statusFilter.value === "passed" && result.status !== "passed") return false;
      if (statusFilter.value === "failed" && result.status !== "failed") return false;
      if (statusFilter.value === "not_tested" && !["not_tested", "deferred", "stale"].includes(result.status)) return false;
    }
    if (!term) return true;
    return [node.hostname, node.ip, node.country, node.country_code, node.asn, node.isp]
      .join(" ")
      .toLowerCase()
      .includes(term);
  });
});

const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / PAGE_SIZE)));
const pageNodes = computed(() => filtered.value.slice((page.value - 1) * PAGE_SIZE, page.value * PAGE_SIZE));

watch([search, countryFilter, statusFilter], () => {
  page.value = 1;
});

watch(pageCount, (value) => {
  if (page.value > value) page.value = value;
});

const allVisibleSelected = computed(() => pageNodes.value.length > 0 && pageNodes.value.every((node) => selected.value.has(node.hostname)));

function toggleVisible(checked: boolean) {
  const next = new Set(selected.value);
  for (const node of pageNodes.value) {
    if (checked) next.add(node.hostname);
    else next.delete(node.hostname);
  }
  selected.value = next;
}

function toggleNode(host: string, checked: boolean) {
  const next = new Set(selected.value);
  if (checked) next.add(host);
  else next.delete(host);
  selected.value = next;
}

const checkRunning = computed(() => summary.value?.check?.status === "running");
const checkPercent = computed(() => {
  const check = summary.value?.check;
  if (!check?.total) return 0;
  return (check.done / check.total) * 100;
});

const refreshMutation = useMutation({
  mutationFn: () => post("api/console/refresh", {}),
  onSuccess: () => {
    toast("正在刷新来源，完成后会自动更新候选列表");
    void queryClient.invalidateQueries({ queryKey: keys.console });
    void queryClient.invalidateQueries({ queryKey: keys.nodes });
  },
  onError: (error) => toastError(error, "刷新失败"),
});

const checkMutation = useMutation({
  mutationFn: (body: Record<string, unknown>) => post("api/console/check", body),
  onMutate: (body) => {
    const hosts = (body.hosts as string[] | undefined) || [];
    for (const host of hosts) submitting.add(host);
  },
  onSuccess: () => {
    toast("检测任务已提交");
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "提交检测失败"),
  onSettled: (_, __, body) => {
    const hosts = (body.hosts as string[] | undefined) || [];
    for (const host of hosts) submitting.delete(host);
  },
});

const cancelMutation = useMutation({
  mutationFn: () => post("api/console/check/cancel", {}),
  onSuccess: () => toast("正在取消检测并清理临时连接"),
  onError: (error) => toastError(error, "取消失败"),
});

const parallelismMutation = useMutation({
  mutationFn: () => post("api/console/check/settings", { parallelism: Number(parallelism.value) }),
  onSuccess: () => {
    parallelismDirty.value = false;
    toast("检测并发已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "保存并发失败"),
});

function checkSelected() {
  if (!selected.value.size) return;
  checkMutation.mutate({ hosts: [...selected.value], priority: true });
}

function checkAll() {
  checkMutation.mutate({ kind: "candidates", all: true });
}

function checkNode(node: Node) {
  checkMutation.mutate({ hosts: [node.hostname], priority: true });
}

// Source settings
const sourceOpen = ref(false);
const sourceLoading = ref(false);
const sourceSaving = ref(false);
const sourceForm = reactive({ mirror_enabled: true, source_timeout_seconds: 10, mirror_max_age_hours: 24 });

async function openSource() {
  sourceLoading.value = true;
  try {
    const value = await api<typeof sourceForm>("api/console/source-settings");
    Object.assign(sourceForm, value);
    sourceOpen.value = true;
  } catch (error) {
    toastError(error, "读取来源设置失败");
  } finally {
    sourceLoading.value = false;
  }
}

async function saveSource() {
  sourceSaving.value = true;
  try {
    await post("api/console/source-settings", {
      mirror_enabled: sourceForm.mirror_enabled,
      source_timeout_seconds: Number(sourceForm.source_timeout_seconds),
      mirror_max_age_hours: Number(sourceForm.mirror_max_age_hours),
    });
    sourceOpen.value = false;
    toast("来源设置已保存，下次刷新生效", { tone: "success" });
  } catch (error) {
    toastError(error, "保存来源设置失败");
  } finally {
    sourceSaving.value = false;
  }
}

// Automation
const automationOpen = ref(false);
const automationLoading = ref(false);
const automationSaving = ref(false);
const automationForm = reactive({ refresh_enabled: true, refresh_minutes: 60, check_enabled: true, check_minutes: 60 });

async function openAutomation() {
  automationLoading.value = true;
  try {
    const value = await api<AutomationView>("api/console/automation");
    Object.assign(automationForm, value.settings);
    automationOpen.value = true;
  } catch (error) {
    toastError(error, "读取周期任务失败");
  } finally {
    automationLoading.value = false;
  }
}

async function saveAutomation() {
  const refresh = Number(automationForm.refresh_minutes);
  const check = Number(automationForm.check_minutes);
  if (![refresh, check].every((value) => Number.isSafeInteger(value) && value >= 30 && value <= 525600)) {
    toast("自动任务间隔须为 30–525600 分钟", { tone: "error" });
    return;
  }
  automationSaving.value = true;
  try {
    await post("api/console/automation", {
      refresh_enabled: automationForm.refresh_enabled,
      refresh_minutes: refresh,
      check_enabled: automationForm.check_enabled,
      check_minutes: check,
    });
    automationOpen.value = false;
    toast("周期任务已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  } catch (error) {
    toastError(error, "保存周期任务失败");
  } finally {
    automationSaving.value = false;
  }
}

const sourceText = computed(() => sourceLabel(summary.value?.pool_source));

// Details
const detailNode = ref<Node | null>(null);
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="节点池" description="来源、候选、检测和周期任务集中在一页；候选数量不等于可用出口数。">
      <template #actions>
        <UiButton :loading="sourceLoading" @click="openSource"><Settings2 :size="15" />来源设置</UiButton>
        <UiButton :loading="automationLoading" @click="openAutomation"><TimerReset :size="15" />周期任务</UiButton>
        <UiButton :loading="refreshMutation.isPending.value || summary?.refresh?.status === 'running'" @click="refreshMutation.mutate()">
          <RefreshCw :size="15" />刷新来源
        </UiButton>
      </template>
    </PageHeader>

    <div v-if="isError" class="rounded-xl border border-bad/25 bg-bad-soft px-4 py-3 text-sm text-bad" role="alert">
      无法读取候选列表：{{ error?.message || "请稍后重试" }}
      <button type="button" class="ml-2 font-semibold underline" @click="refetch()">重试</button>
    </div>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
      <div class="min-w-0 space-y-4">
        <div class="flex flex-wrap items-end gap-2">
          <label class="relative min-w-[220px] flex-1">
            <Search :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-faint" />
            <input v-model="search" name="node-search" type="search" placeholder="搜索 IP、主机名、ASN 或国家" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
          </label>
          <select v-model="countryFilter" name="node-country" class="h-9 min-w-36 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选来源国家">
            <option value="">所有来源国家</option>
            <option v-for="[code, name] in countries" :key="code" :value="code">{{ name }}（{{ code }}）</option>
          </select>
          <select v-model="statusFilter" name="node-status" class="h-9 min-w-32 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选检测状态">
            <option value="all">所有状态</option>
            <option value="passed">通过</option>
            <option value="failed">未通过</option>
            <option value="not_tested">未检测 / 过期</option>
          </select>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <UiButton size="sm" :disabled="!selected.size" :loading="checkMutation.isPending.value" @click="checkSelected">
            <ShieldCheck :size="14" />检测所选（{{ selected.size }}）
          </UiButton>
          <UiButton size="sm" :loading="checkMutation.isPending.value" @click="checkAll">检测全部候选</UiButton>
          <UiButton v-if="checkRunning" size="sm" variant="ghost" :loading="cancelMutation.isPending.value" @click="cancelMutation.mutate()">
            <X :size="13" />取消检测
          </UiButton>
          <div class="ml-auto flex items-center gap-1.5">
            <label class="flex items-center gap-1.5 text-[11px] text-muted">
              检测并发
              <input
                v-model.number="parallelism"
                name="check-parallelism"
                type="number"
                min="1"
                class="h-8 w-16 rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent"
                @input="parallelismDirty = true"
              />
            </label>
            <UiButton size="sm" :disabled="!parallelismDirty" :loading="parallelismMutation.isPending.value" @click="parallelismMutation.mutate()">保存</UiButton>
          </div>
        </div>

        <Panel flush>
          <div v-if="summary?.check" class="border-b border-line bg-surface-2 px-4 py-3">
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="font-medium">候选检测 · {{ summary.check.done }} / {{ summary.check.total }}</span>
              <StatusBadge :label="checkStatusLabel(summary.check.status)" :tone="checkStatusTone(summary.check.status)" size="sm" :pulse="summary.check.status === 'running'" />
            </div>
            <ProgressBar :value="checkPercent" size="sm" class="mt-2" label="候选检测进度" />
          </div>

          <div v-if="isLoading && !view" class="p-4"><SkeletonBlock :lines="8" /></div>
          <div v-else-if="!filtered.length" class="p-4">
            <EmptyState title="没有符合条件的候选" description="调整筛选条件，或刷新来源获取新的候选清单。">
              <template #icon><Database :size="26" /></template>
              <template #action>
                <UiButton size="sm" :loading="refreshMutation.isPending.value" @click="refreshMutation.mutate()">
                  <RefreshCw :size="14" />刷新来源
                </UiButton>
              </template>
            </EmptyState>
          </div>
          <p v-else class="px-4 pt-2 text-[11px] text-faint sm:hidden">左右滑动查看完整列 →</p>
          <div v-if="filtered.length" class="overflow-x-auto">
            <table class="w-full min-w-[900px] border-collapse text-left">
              <caption class="sr-only">VPN Gate 候选节点列表</caption>
              <thead>
                <tr class="border-b border-line text-[11px] text-faint">
                  <th class="w-10 px-4 py-2">
                    <input type="checkbox" name="select-visible" :checked="allVisibleSelected" aria-label="选择当前页" class="accent-accent" @change="toggleVisible(($event.target as HTMLInputElement).checked)" />
                  </th>
                  <th class="px-2 py-2 font-medium">来源节点</th>
                  <th class="px-2 py-2 font-medium">来源地区</th>
                  <th class="px-2 py-2 font-medium">实际出口</th>
                  <th class="px-2 py-2 font-medium">检测结果</th>
                  <th class="px-2 py-2 font-medium">HTTPS 延迟</th>
                  <th class="px-2 py-2 font-medium">检测时间</th>
                  <th class="px-4 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="node in pageNodes" :key="node.hostname" class="border-b border-line last:border-b-0 hover:bg-surface-2">
                  <td class="px-4 py-2.5">
                    <input type="checkbox" name="select-node" :checked="selected.has(node.hostname)" :aria-label="`选择 ${node.hostname}`" class="accent-accent" @change="toggleNode(node.hostname, ($event.target as HTMLInputElement).checked)" />
                  </td>
                  <td class="px-2 py-2.5">
                    <button type="button" class="min-w-0 text-left" @click="detailNode = node">
                      <span class="block truncate font-mono text-[11px] font-medium text-ink hover:text-accent">{{ truncate(node.hostname, 30) }}</span>
                      <span class="block truncate text-[11px] text-faint">{{ node.ip }}<template v-if="node.asn"> · {{ node.asn }}</template><template v-if="node.isp"> · {{ truncate(node.isp, 20) }}</template></span>
                    </button>
                  </td>
                  <td class="px-2 py-2.5 text-xs">
                    <span class="font-medium">{{ countryName(node.country_code) }}</span>
                    <span class="ml-1 text-[10px] text-faint">{{ node.country_code }}</span>
                    <div class="text-[10px] text-faint">{{ node.residential ? "志愿者家宽" : "VPN Gate 自营" }}</div>
                  </td>
                  <td class="px-2 py-2.5 text-xs">
                    <span class="block font-mono text-[11px]">{{ effectiveResult(node).actual_ip || node.expected_exit_ip || "—" }}</span>
                    <span class="text-[10px] text-faint">{{ effectiveResult(node).country ? countryName(effectiveResult(node).country) : ipTypeLabel(node.ip_type) }}</span>
                  </td>
                  <td class="px-2 py-2.5">
                    <StatusBadge :label="checkStatusLabel(effectiveResult(node).status)" :tone="checkStatusTone(effectiveResult(node).status)" size="sm" :pulse="effectiveResult(node).status === 'running'" />
                    <div v-if="effectiveResult(node).code" class="mt-1 text-[10px] text-bad">{{ errorLabel(effectiveResult(node).code) }}</div>
                  </td>
                  <td class="tnum px-2 py-2.5 text-xs text-muted">{{ formatLatency(effectiveResult(node).latency_ms) }}</td>
                  <td class="px-2 py-2.5 text-[11px] text-faint">{{ formatDateTime(effectiveResult(node).checked_at) }}</td>
                  <td class="px-4 py-2.5 text-right">
                    <UiButton size="sm" variant="ghost" :loading="submitting.has(node.hostname)" @click="checkNode(node)">检测</UiButton>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="filtered.length > PAGE_SIZE" class="flex items-center justify-between gap-2 border-t border-line px-4 py-2.5 text-[11px] text-muted">
            <span>共 {{ filtered.length }} 个候选 · 第 {{ page }} / {{ pageCount }} 页</span>
            <div class="flex gap-1">
              <UiButton size="sm" variant="ghost" :disabled="page <= 1" @click="page -= 1">上一页</UiButton>
              <UiButton size="sm" variant="ghost" :disabled="page >= pageCount" @click="page += 1">下一页</UiButton>
            </div>
          </div>
        </Panel>
      </div>

      <aside class="min-w-0 space-y-4">
        <Panel title="来源与任务">
          <dl class="space-y-3 text-xs">
            <div><dt class="text-faint">来源</dt><dd class="mt-0.5 leading-relaxed text-ink">{{ sourceText }}</dd></div>
            <div><dt class="text-faint">来源刷新</dt><dd class="mt-0.5 text-ink">{{ scheduleLabel(summary?.automation, "refresh") }}</dd></div>
            <div><dt class="text-faint">候选检测</dt><dd class="mt-0.5 text-ink">{{ scheduleLabel(summary?.automation, "check") }}</dd></div>
            <div><dt class="text-faint">上次获取</dt><dd class="mt-0.5 text-ink">{{ formatDateTime(summary?.pool_generated_at) }}</dd></div>
          </dl>
          <p class="mt-3 border-t border-line pt-3 text-[11px] leading-relaxed text-faint">
            所有获取、镜像校验和节点检测都由本 VPS 执行；检测通过不等于会永久在线。
          </p>
        </Panel>

        <Panel title="筛选结果">
          <div class="grid grid-cols-2 gap-3 text-xs">
            <div><div class="text-[11px] text-faint">当前结果</div><div class="tnum mt-0.5 text-lg font-semibold">{{ filtered.length }}</div></div>
            <div><div class="text-[11px] text-faint">已选择</div><div class="tnum mt-0.5 text-lg font-semibold">{{ selected.size }}</div></div>
          </div>
          <UiButton v-if="selected.size" size="sm" variant="ghost" class="mt-3" @click="selected = new Set()">清空选择</UiButton>
        </Panel>
      </aside>
    </div>

    <DialogShell :open="sourceOpen" title="候选来源设置" description="所有清单获取和节点检测都由本 VPS 执行。" size="md" @update:open="sourceOpen = $event">
      <form class="space-y-4" @submit.prevent="saveSource">
        <label class="flex items-start justify-between gap-4 rounded-lg border border-line p-3">
          <span>
            <span class="block text-sm font-medium text-ink">允许第三方镜像后备</span>
            <span class="mt-0.5 block text-[11px] leading-relaxed text-muted">仅在当前来源失败时请求镜像原始 CSV；不采用镜像的检测结论。</span>
          </span>
          <ToggleSwitch v-model="sourceForm.mirror_enabled" label="允许第三方镜像后备" />
        </label>
        <label class="block">
          <span class="mb-1 block text-xs font-medium">每次来源请求最长等待（秒）</span>
          <input v-model.number="sourceForm.source_timeout_seconds" name="source-timeout" type="number" min="2" max="20" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <label class="block">
          <span class="mb-1 block text-xs font-medium">镜像声明抓取时间的最大年龄（小时）</span>
          <input v-model.number="sourceForm.mirror_max_age_hours" name="mirror-max-age" type="number" min="1" max="168" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          <span class="mt-1 block text-[11px] text-faint">默认 24 小时；超过 3 小时会提示较旧。校验失败时保留旧清单。</span>
        </label>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="sourceOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="sourceSaving">保存</UiButton>
        </div>
      </form>
    </DialogShell>

    <DialogShell :open="automationOpen" title="周期任务" description="自动任务最低 30 分钟，默认 60 分钟；获取和检测由本 VPS 完成。" size="md" @update:open="automationOpen = $event">
      <form class="space-y-4" @submit.prevent="saveAutomation">
        <div class="rounded-lg border border-line p-3">
          <div class="flex items-center justify-between gap-3">
            <div><div class="text-sm font-medium">自动刷新候选清单</div><div class="text-[11px] text-muted">失败保留旧清单。</div></div>
            <ToggleSwitch v-model="automationForm.refresh_enabled" label="自动刷新候选清单" />
          </div>
          <label class="mt-3 block">
            <span class="mb-1 block text-[11px] text-faint">刷新间隔（分钟）</span>
            <input v-model.number="automationForm.refresh_minutes" name="refresh-minutes" type="number" min="30" max="525600" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
        </div>
        <div class="rounded-lg border border-line p-3">
          <div class="flex items-center justify-between gap-3">
            <div><div class="text-sm font-medium">自动批量检测候选</div><div class="text-[11px] text-muted">按完成时间计算下一轮，不叠加任务。</div></div>
            <ToggleSwitch v-model="automationForm.check_enabled" label="自动批量检测候选" />
          </div>
          <label class="mt-3 block">
            <span class="mb-1 block text-[11px] text-faint">检测间隔（分钟）</span>
            <input v-model.number="automationForm.check_minutes" name="check-minutes" type="number" min="30" max="525600" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="automationOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="automationSaving">保存</UiButton>
        </div>
      </form>
    </DialogShell>

    <DialogShell :open="Boolean(detailNode)" :title="detailNode?.hostname || '节点详情'" description="来源节点与检测观测" size="md" @update:open="detailNode = $event ? detailNode : null">
      <div v-if="detailNode" class="space-y-4">
        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
          <div><dt class="text-faint">服务器 IP</dt><dd class="mt-0.5 font-mono">{{ detailNode.ip }}</dd></div>
          <div><dt class="text-faint">来源地区</dt><dd class="mt-0.5">{{ countryName(detailNode.country_code) }}（{{ detailNode.country_code }}）</dd></div>
          <div><dt class="text-faint">ASN</dt><dd class="mt-0.5">{{ detailNode.asn || "—" }}</dd></div>
          <div><dt class="text-faint">ISP</dt><dd class="mt-0.5">{{ detailNode.isp || "—" }}</dd></div>
          <div><dt class="text-faint">候选类型</dt><dd class="mt-0.5">{{ detailNode.residential ? "志愿者家宽" : "VPN Gate 自营" }}</dd></div>
          <div><dt class="text-faint">IP 类型</dt><dd class="mt-0.5">{{ ipTypeLabel(detailNode.ip_type) }}</dd></div>
          <div><dt class="text-faint">预期出口</dt><dd class="mt-0.5 font-mono">{{ detailNode.expected_exit_ip || "—" }}</dd></div>
          <div><dt class="text-faint">SSTP 候选</dt><dd class="mt-0.5">{{ detailNode.sstp_candidates?.length || 0 }} 个</dd></div>
          <div><dt class="text-faint">Ping</dt><dd class="tnum mt-0.5">{{ detailNode.ping ? `${detailNode.ping} ms` : "—" }}</dd></div>
          <div><dt class="text-faint">速度</dt><dd class="tnum mt-0.5">{{ formatMbps(detailNode.speed_mbps) }}</dd></div>
        </dl>
        <div class="rounded-lg border border-line p-3 text-xs">
          <div class="mb-1 text-faint">当前观测</div>
          <StatusBadge :label="checkStatusLabel(effectiveResult(detailNode).status)" :tone="checkStatusTone(effectiveResult(detailNode).status)" size="sm" />
          <p v-if="effectiveResult(detailNode).code" class="mt-2 text-bad">{{ errorLabel(effectiveResult(detailNode).code) }}</p>
          <p class="mt-2 text-faint">检测时间 {{ formatDateTime(effectiveResult(detailNode).checked_at) }}</p>
        </div>
        <UiButton size="sm" :loading="submitting.has(detailNode.hostname)" @click="checkNode(detailNode)">立即检测</UiButton>
      </div>
    </DialogShell>
  </div>
</template>
