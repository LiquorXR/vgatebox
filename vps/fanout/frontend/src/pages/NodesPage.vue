<script setup lang="ts">
import { Database, RefreshCw, Search, Settings2, ShieldCheck, TimerReset, X } from "@lucide/vue";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { api, post } from "@/api/client";
import { keys, useConsoleQuery } from "@/api/queries";
import type { CheckResult, EgressProfile, Node, NodesView } from "@/api/types";
import EmptyState from "@/components/EmptyState.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import AutomationDialog from "@/pages/nodes/AutomationDialog.vue";
import NodeCheckProgress from "@/pages/nodes/NodeCheckProgress.vue";
import NodeDetailDialog from "@/pages/nodes/NodeDetailDialog.vue";
import NodeFilterBar from "@/pages/nodes/NodeFilterBar.vue";
import NodeSidebar from "@/pages/nodes/NodeSidebar.vue";
import NodeTableBody from "@/pages/nodes/NodeTableBody.vue";
import SourceSettingsDialog from "@/pages/nodes/SourceSettingsDialog.vue";
import { countryName } from "@/lib/format";
import { sourceLabel } from "@/lib/automation";
import { checkStatusLabel, checkStatusTone } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const PAGE_SIZE = 50;
// 服务端分页：首屏拉 200，超了点“加载更多”按 next_offset 续拉累加。
const SERVER_PAGE = 200;
const queryClient = useQueryClient();
const { data: summary } = useConsoleQuery();
const {
  data: nodePages,
  isLoading,
  isError,
  error,
  refetch,
  fetchNextPage,
  hasNextPage,
  isFetchingNextPage,
} = useInfiniteQuery({
  queryKey: [...keys.nodes, SERVER_PAGE],
  queryFn: ({ pageParam }) => api<NodesView>(`api/nodes?all=1&limit=${SERVER_PAGE}&offset=${pageParam}`),
  initialPageParam: 0,
  getNextPageParam: (lastPage) => lastPage.next_offset ?? undefined,
  refetchInterval: 20000,
  refetchIntervalInBackground: false,
  staleTime: 5000,
});

const nodes = computed(() => (nodePages.value?.pages ?? []).flatMap((page) => page.nodes));
const serverTotal = computed(() => nodePages.value?.pages[0]?.total ?? 0);
const search = ref("");
const countryFilter = ref("");
const statusFilter = ref("all");
const typeFilter = ref("all");
const page = ref(1);
const selected = ref<Set<string>>(new Set());
const sourceOpen = ref(false);
const automationOpen = ref(false);
const parallelism = ref(2);
const parallelismDirty = ref(false);
const ipTypeCheck = ref(false);
const ipTypeCheckDirty = ref(false);
const ipTypeTTLHours = ref(24);
const ipTypeTTLDirty = ref(false);
const submitting = reactive(new Set<string>());

watch(
  () => summary.value?.check_settings?.parallelism,
  (value) => {
    if (value && !parallelismDirty.value) parallelism.value = value;
  },
  { immediate: true },
);

watch(
  () => summary.value?.check_settings?.ip_type_check,
  (value) => {
    if (typeof value === "boolean" && !ipTypeCheckDirty.value) ipTypeCheck.value = value;
  },
  { immediate: true },
);

watch(
  () => summary.value?.check_settings?.ip_type_ttl_hours,
  (value) => {
    if (typeof value === "number" && !ipTypeTTLDirty.value) ipTypeTTLHours.value = value;
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

// 类型列优先使用最近一次检测的观测；没有检测结果时退回候选池里的证据。
// 旧持久化结果可能带已移除的类型，统一归入类型未知。
function nodeIPType(node: Node): string {
  const value = effectiveResult(node).ip_type || node.ip_type;
  return value === "strict_residential" ? "strict_residential" : "unknown";
}

// 检测结果里的画像是本次临时隧道刚测到的，优先于候选池里的旧快照。
function nodeEgress(node: Node): EgressProfile | null | undefined {
  return effectiveResult(node).egress || node.openvpn_egress;
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
    if (typeFilter.value !== "all" && nodeIPType(node) !== typeFilter.value) return false;
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
const pageRows = computed(() =>
  pageNodes.value.map((node) => ({ node, result: effectiveResult(node), ipType: nodeIPType(node) })),
);

watch([search, countryFilter, statusFilter, typeFilter], () => {
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

const checkSettingsMutation = useMutation({
  mutationFn: () => post("api/console/check/settings", {
    parallelism: Number(parallelism.value),
    ip_type_check: ipTypeCheck.value,
    ip_type_ttl_hours: Number(ipTypeTTLHours.value),
  }),
  onSuccess: () => {
    parallelismDirty.value = false;
    ipTypeCheckDirty.value = false;
    ipTypeTTLDirty.value = false;
    toast("检测设置已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "保存检测设置失败"),
});

const clearIPTypeMutation = useMutation({
  mutationFn: () => post("api/console/ip-type-cache", {}),
  onSuccess: () => {
    toast("IP 类型缓存已清除", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "清除 IP 类型缓存失败"),
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

const sourceText = computed(() => sourceLabel(summary.value?.pool_source));

const checkSettings = computed(() => summary.value?.check_settings);
const ipTypeCache = computed(() => checkSettings.value?.ip_type_cache);
// The effective TTL already covers a full batch plus the next wait; show the
// measured round so a slow pool is understandable instead of surprising.
const checkCycleMinutes = computed(() => {
  const seconds = checkSettings.value?.check_cycle_seconds;
  if (!seconds) return 0;
  return Math.max(1, Math.round(seconds / 60));
});

function observationExpiry(node: Node): string {
  const result = effectiveResult(node);
  const minutes = checkSettings.value?.observation_ttl_effective_minutes;
  if (!result.checked_at || !minutes) return "";
  const date = new Date(result.checked_at);
  if (Number.isNaN(date.getTime())) return "";
  return new Date(date.getTime() + minutes * 60_000).toISOString();
}

// Details
const detailNode = ref<Node | null>(null);
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="节点池" description="来源、候选、检测和周期任务集中在一页；候选数量不等于可用出口数。">
      <template #actions>
        <UiButton @click="sourceOpen = true"><Settings2 :size="15" />来源设置</UiButton>
        <UiButton @click="automationOpen = true"><TimerReset :size="15" />周期任务</UiButton>
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
        <NodeFilterBar
          v-model:search="search"
          v-model:country-filter="countryFilter"
          v-model:status-filter="statusFilter"
          v-model:type-filter="typeFilter"
          :countries="countries"
        />

        <div class="flex flex-wrap items-center gap-2">
          <UiButton size="sm" :disabled="!selected.size" :loading="checkMutation.isPending.value" @click="checkSelected">
            <ShieldCheck :size="14" />检测所选（{{ selected.size }}）
          </UiButton>
          <UiButton size="sm" :loading="checkMutation.isPending.value" @click="checkAll">检测全部候选</UiButton>
          <UiButton v-if="checkRunning" size="sm" variant="ghost" :loading="cancelMutation.isPending.value" @click="cancelMutation.mutate()">
            <X :size="13" />取消检测
          </UiButton>
          <div class="flex flex-wrap items-center gap-2 md:ml-auto">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg border border-line bg-surface-2 px-2.5 py-1">
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
            </div>
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg border border-line bg-surface-2 px-2.5 py-1">
              <span class="text-[11px] text-faint">检测 IP 类型</span>
              <label class="flex items-center gap-1.5 text-[11px] text-muted" title="开启后手动和自动候选检测会查询 IPPure；出口换 IP 始终检查类型">
                <ToggleSwitch v-model="ipTypeCheck" label="检测 IP 类型" @update:model-value="ipTypeCheckDirty = true" />
              </label>
              <label class="flex items-center gap-1.5 text-[11px] text-muted" title="有效 IP 类型条目的缓存时长，1–168 小时">
                缓存有效期（小时）
                <input
                  v-model.number="ipTypeTTLHours"
                  name="check-iptype-ttl"
                  type="number"
                  min="1"
                  max="168"
                  class="h-8 w-16 rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent"
                  @input="ipTypeTTLDirty = true"
                />
              </label>
            </div>
            <UiButton size="sm" :disabled="!parallelismDirty && !ipTypeCheckDirty && !ipTypeTTLDirty" :loading="checkSettingsMutation.isPending.value" @click="checkSettingsMutation.mutate()">保存</UiButton>
          </div>
        </div>

        <Panel flush>
          <NodeCheckProgress :check="summary?.check" :percent="checkPercent" />

          <div v-if="isLoading && !nodePages" class="p-4"><SkeletonBlock :lines="8" /></div>
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
            <table class="w-full min-w-[1000px] border-collapse text-left">
              <caption class="sr-only">VPN Gate 候选节点列表</caption>
              <thead>
                <tr class="border-b border-line text-[11px] text-faint">
                  <th class="w-10 px-4 py-2">
                    <input type="checkbox" name="select-visible" :checked="allVisibleSelected" aria-label="选择当前页" class="accent-accent" @change="toggleVisible(($event.target as HTMLInputElement).checked)" />
                  </th>
                  <th class="px-2 py-2 font-medium">来源节点</th>
                  <th class="px-2 py-2 font-medium">来源地区</th>
                  <th class="px-2 py-2 font-medium">实际出口</th>
                  <th class="px-2 py-2 font-medium">IP 类型</th>
                  <th class="px-2 py-2 font-medium">检测结果</th>
                  <th class="px-2 py-2 font-medium">HTTPS 延迟</th>
                  <th class="px-2 py-2 font-medium">检测时间</th>
                  <th class="px-4 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                <NodeTableBody
                  :rows="pageRows"
                  :selected="selected"
                  :submitting="submitting"
                  @toggle-node="toggleNode"
                  @open-detail="detailNode = $event"
                  @check-node="checkNode"
                />
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
          <div v-if="hasNextPage" class="flex items-center justify-between gap-2 border-t border-line px-4 py-2.5 text-[11px] text-muted">
            <span>已加载 {{ nodes.length }} / 共 {{ serverTotal }} 个候选</span>
            <UiButton size="sm" variant="ghost" :loading="isFetchingNextPage" @click="fetchNextPage()">加载更多候选</UiButton>
          </div>
        </Panel>
      </div>

      <NodeSidebar
        :summary="summary"
        :filtered-count="filtered.length"
        :selected-count="selected.size"
        :source-text="sourceText"
        :check-settings="checkSettings"
        :check-cycle-minutes="checkCycleMinutes"
        :ip-type-cache="ipTypeCache"
        :clearing-cache="clearIPTypeMutation.isPending.value"
        @clear-cache="clearIPTypeMutation.mutate()"
        @clear-selection="selected = new Set()"
      />
    </div>

    <SourceSettingsDialog v-model:open="sourceOpen" />
    <AutomationDialog v-model:open="automationOpen" />
    <NodeDetailDialog
      :open="Boolean(detailNode)"
      :node="detailNode"
      :result="detailNode ? effectiveResult(detailNode) : null"
      :egress="detailNode ? nodeEgress(detailNode) : null"
      :ip-type="detailNode ? nodeIPType(detailNode) : 'unknown'"
      :observation-expiry="detailNode ? observationExpiry(detailNode) : ''"
      :submitting="detailNode ? submitting.has(detailNode.hostname) : false"
      @update:open="detailNode = $event ? detailNode : null"
      @check="detailNode && checkNode(detailNode)"
    />
  </div>
</template>
