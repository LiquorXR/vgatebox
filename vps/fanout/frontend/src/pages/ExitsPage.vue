<script setup lang="ts">
import { ArrowLeftRight, ChevronRight, RefreshCw, Search, ShieldCheck, SlidersHorizontal, X } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { post, queryString } from "@/api/client";
import { keys, useConsoleQuery, useExitsQuery, useNodesQuery } from "@/api/queries";
import type { Exit } from "@/api/types";
import DialogShell from "@/components/DialogShell.vue";
import EmptyState from "@/components/EmptyState.vue";
import LatencyTrend from "@/components/LatencyTrend.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SectionTabs from "@/components/SectionTabs.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import Spinner from "@/components/Spinner.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import ExitDetailDrawer from "@/pages/exit/ExitDetailDrawer.vue";
import ExitTargetDialog from "@/pages/exit/ExitTargetDialog.vue";
import { lastExitText as lastExitTextAt, retryText as retryTextAt, useNow } from "@/lib/exit-time";
import { countryName, formatLatency, pad2 } from "@/lib/format";
import { toast, toastError } from "@/lib/toast";
import {
  countryTag,
  directDisplayName,
  directEnabled,
  directPorts,
  exitCheck,
  exitKind,
  exitName,
  exitStatusLabel,
  exitStatusTone,
  exitSubLabel,
  phaseLabels,
} from "@/lib/status";

const route = useRoute();
const router = useRouter();
const queryClient = useQueryClient();
const { data: summary } = useConsoleQuery();
const { data: view, isLoading, isError, error, refetch } = useExitsQuery(true);
const { data: nodesView } = useNodesQuery(true);

const exits = computed(() => view.value?.exits || []);
const hostDirect = computed(() => view.value?.host_direct || []);
const directOn = computed(() => directEnabled(hostDirect.value));
const target = computed(() => summary.value?.target || 0);
const ready = computed(() => summary.value?.ready || 0);
const waiting = computed(() => summary.value?.waiting || 0);
const stopped = computed(() => summary.value?.stopped || 0);
const allocated = computed(() => summary.value?.allocated || exits.value.length);

const kindFilter = ref("all");
const statusFilter = ref("all");
const search = ref("");

const tabs = computed(() => [
  { key: "all", label: "全部", count: exits.value.length + (directOn.value ? 1 : 0) },
  { key: "vpn", label: "VPN Gate", count: exits.value.filter((exit) => exitKind(exit) === "vpn").length },
  { key: "custom", label: "自定义代理", count: exits.value.filter((exit) => exitKind(exit) === "custom").length },
  { key: "direct", label: "本机网络", count: directOn.value ? 1 : 0 },
]);

const rows = computed(() => {
  const term = search.value.trim().toLowerCase();
  return exits.value.filter((exit) => {
    if (kindFilter.value !== "all" && kindFilter.value !== "direct" && exitKind(exit) !== kindFilter.value) return false;
    if (kindFilter.value === "direct") return false;
    if (statusFilter.value !== "all") {
      if (statusFilter.value === "waiting") {
        if (!["waiting", "waiting_for_nodes"].includes(exit.status)) return false;
      } else if (exit.status !== statusFilter.value) return false;
    }
    if (!term) return true;
    return [exitName(exit), exit.host, exit.exit_ip, exit.last_exit_ip, exit.country, exit.region, String(exit.slot)]
      .join(" ")
      .toLowerCase()
      .includes(term);
  });
});

const showDirect = computed(() => (kindFilter.value === "all" || kindFilter.value === "direct") && directOn.value);

const check = computed(() => summary.value?.check || null);
const checkRunning = computed(() => check.value?.status === "running");
const checkPercent = computed(() => {
  if (!check.value?.total) return 0;
  return (check.value.done / check.value.total) * 100;
});
const checkCurrent = computed(() =>
  (check.value?.items || [])
    .filter((item) => item.status === "running")
    .slice(0, 3)
    .map((item) => `${item.slot ? `出口 ${pad2(item.slot)}` : item.host}${item.phase ? ` · ${phaseLabels[item.phase] || item.phase}` : ""}`),
);

function checkItemFor(exit: Exit) {
  return check.value?.items?.find((item) => item.slot === exit.slot || (item.host && item.host === exit.host)) || null;
}

function checkItemState(exit: Exit): { label: string; running: boolean } | null {
  const item = checkItemFor(exit);
  if (!item) return null;
  if (item.status === "running") return { label: phaseLabels[item.phase || ""] || "检测中", running: true };
  if (["queued", "priority_queued"].includes(item.status)) return { label: "排队中", running: false };
  return null;
}

const nowMs = useNow();
const retryText = (exit: Exit) => retryTextAt(exit, nowMs.value);
const lastExitText = (exit: Exit) => lastExitTextAt(exit, nowMs.value);

// Target dialog
const targetOpen = ref(false);

function openTarget() {
  if (!summary.value) {
    toast("请等待状态读取完成");
    return;
  }
  targetOpen.value = true;
}

// Check all
const checkAllMutation = useMutation({
  mutationFn: () => {
    const slots = exits.value.filter((exit) => exit.status === "up").map((exit) => exit.slot);
    if (!slots.length) throw new Error("当前没有就绪出口");
    return post("api/console/check", { kind: "exits", slots });
  },
  onSuccess: () => {
    toast("检测任务已提交");
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "提交检测失败"),
});

const cancelCheckMutation = useMutation({
  mutationFn: () => post("api/console/check/cancel", {}),
  onSuccess: () => {
    toast("正在取消检测");
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "取消失败"),
});

// Drawer
const drawerOpen = ref(false);
const activeSlot = ref(0);
const selectedExit = computed(() => exits.value.find((exit) => exit.slot === activeSlot.value) || null);

const countryOptions = computed(() => {
  const map = new Map<string, string>();
  for (const node of nodesView.value?.nodes || []) {
    if (node.country_code) map.set(node.country_code, node.country || countryName(node.country_code));
  }
  for (const exit of exits.value) {
    if (exit.region) map.set(exit.region, countryName(exit.region));
  }
  return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
});

watch(
  () => route.query.slot,
  (value) => {
    const slot = Number(value);
    if (slot > 0) {
      activeSlot.value = slot;
      drawerOpen.value = true;
    } else {
      drawerOpen.value = false;
      activeSlot.value = 0;
    }
  },
  { immediate: true },
);

function openExit(exit: Exit) {
  activeSlot.value = exit.slot;
  drawerOpen.value = true;
  void router.replace({ path: "/exits", query: { slot: String(exit.slot) } });
}

function setDrawer(open: boolean) {
  drawerOpen.value = open;
  if (!open) {
    void router.replace({ path: "/exits" });
  }
}

const directInbound = computed(() => {
  const list = hostDirect.value || [];
  return list.find((item) => item.enable) || list[0] || null;
});
const directEditOpen = ref(false);
const directForm = reactive({ mode: "custom", remark: "" });

function openDirectEdit() {
  const current = directInbound.value;
  directForm.mode = current?.name_mode === "auto" ? "auto" : "custom";
  directForm.remark = current?.remark || "";
  directEditOpen.value = true;
}

const directNameMutation = useMutation({
  mutationFn: () => {
    const current = directInbound.value;
    if (!current) throw new Error("直出入口不存在");
    if (directForm.mode === "custom" && !directForm.remark.trim()) throw new Error("请输入自定义名称");
    return post(
      `api/panel/inbound/update${queryString({
        id: current.id,
        name_mode: directForm.mode,
        ...(directForm.mode === "custom" ? { remark: directForm.remark.trim() } : {}),
      })}`,
      {},
    );
  },
  onSuccess: () => {
    toast("直出订阅名称已更新", { tone: "success" });
    directEditOpen.value = false;
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "保存直出名称失败"),
});

const statusOptions = [
  { value: "all", label: "全部状态" },
  { value: "up", label: "已就绪" },
  { value: "starting", label: "连接中" },
  { value: "waiting", label: "等待候选" },
  { value: "degraded", label: "速度不佳" },
  { value: "stopped", label: "已停止" },
  { value: "blocked", label: "已阻断" },
  { value: "error", label: "连接异常" },
];
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="出口" description="统一查看本机网络、自定义代理和 VPN Gate，明确每条连接的去向。">
      <template #actions>
        <UiButton :loading="checkAllMutation.isPending.value" :disabled="ready === 0 || checkRunning" @click="checkAllMutation.mutate()">
          <ShieldCheck :size="15" />检测就绪出口
        </UiButton>
        <UiButton variant="primary" @click="openTarget">
          <SlidersHorizontal :size="15" />调整数量
        </UiButton>
      </template>
    </PageHeader>

    <div class="grid grid-cols-2 gap-2.5 rounded-xl border border-line bg-surface px-4 py-3 sm:grid-cols-4">
      <div><div class="text-[11px] text-muted">目标</div><div class="tnum text-lg font-semibold">{{ target }}</div></div>
      <div><div class="text-[11px] text-muted">已就绪</div><div class="tnum text-lg font-semibold text-ok">{{ ready }}</div></div>
      <div><div class="text-[11px] text-muted">等待 / 停止</div><div class="tnum text-lg font-semibold">{{ waiting }} <span class="text-sm font-normal text-faint">/ {{ stopped }}</span></div></div>
      <div><div class="text-[11px] text-muted">已分配</div><div class="tnum text-lg font-semibold">{{ allocated }}</div></div>
    </div>

    <div v-if="isError" class="rounded-xl border border-bad/25 bg-bad-soft px-4 py-3 text-sm text-bad" role="alert">
      无法读取出口列表：{{ error?.message || "请稍后重试" }}
      <button type="button" class="ml-2 font-semibold underline" @click="refetch()">重试</button>
    </div>

    <div v-if="summary?.plan_error" class="rounded-xl border border-warn/25 bg-warn-soft px-4 py-3 text-sm text-warn" role="status">
      出口计划等待重试：{{ summary.plan_error }}。候选不足时会保留策略等待，不会强行使用其他出口。
    </div>

    <div v-if="checkRunning && check" class="rounded-xl border border-line bg-surface px-4 py-3" role="status" aria-live="polite">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex items-center gap-2 text-xs font-medium text-ink">
          <Spinner :size="14" class="text-accent" />
          正在检测出口
          <span class="tnum font-normal text-muted">{{ check.done }} / {{ check.total }}</span>
        </div>
        <div class="flex items-center gap-2">
          <StatusBadge label="检测中" tone="info" size="sm" pulse />
          <UiButton variant="ghost" size="sm" :loading="cancelCheckMutation.isPending.value" @click="cancelCheckMutation.mutate()">
            <X :size="13" />取消检测
          </UiButton>
        </div>
      </div>
      <ProgressBar :value="checkPercent" class="mt-2.5" label="出口检测进度" />
      <div class="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-faint">
        <span class="truncate">并发 {{ check.parallelism }}<template v-if="checkCurrent.length"> · {{ checkCurrent.join("；") }}</template></span>
        <span>结果会随检测自动更新</span>
      </div>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <SectionTabs v-model="kindFilter" :items="tabs" />
      <label class="relative ml-auto block w-full sm:w-56">
        <Search :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-faint" />
        <input v-model="search" name="exit-search" type="search" autocomplete="off" placeholder="搜索名称、IP 或国家" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
      </label>
      <select v-model="statusFilter" name="exit-status" class="h-9 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选状态">
        <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
    </div>

    <Panel flush>
      <div v-if="isLoading && !view" class="p-4"><SkeletonBlock :lines="6" /></div>
      <div v-else-if="!rows.length && !showDirect" class="p-4">
        <EmptyState title="没有符合条件的出口" description="调整筛选条件，或先刷新候选来源并等待出口就绪。">
          <template #icon><ArrowLeftRight :size="26" /></template>
          <template #action>
            <div class="flex gap-2">
              <UiButton size="sm" @click="kindFilter = 'all'; statusFilter = 'all'; search = ''">清除筛选</UiButton>
              <UiButton size="sm" variant="primary" @click="openTarget">调整数量</UiButton>
            </div>
          </template>
        </EmptyState>
      </div>
      <div v-else>
        <p class="px-4 pt-2 text-[11px] text-faint sm:hidden">左右滑动查看完整列 →</p>
        <div class="overflow-x-auto">
          <div class="min-w-[880px]">
          <div class="grid grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)_72px] gap-4 border-b border-line px-4 py-2 text-[11px] text-faint">
            <span>出口</span><span>状态</span><span>实际出口</span><span class="text-center">延迟</span><span class="text-right">操作</span>
          </div>

          <button
            v-if="showDirect"
            type="button"
            class="grid w-full grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)_72px] items-center gap-4 border-b border-line px-4 py-3 text-left hover:bg-surface-2"
            @click="openDirectEdit"
          >
            <div class="flex min-w-0 items-center gap-2.5">
              <span class="rounded border border-accent bg-accent-soft px-1.5 py-0.5 text-[10px] font-bold text-accent">VPS</span>
              <div class="min-w-0">
                <div class="truncate text-[13px] font-medium">{{ directDisplayName(hostDirect) }}</div>
                <div class="truncate text-[11px] text-faint">本机网络 · 端口 {{ directPorts(hostDirect).join(" / ") || "—" }}</div>
              </div>
            </div>
            <StatusBadge label="已启用" tone="ok" size="sm" />
            <span class="truncate text-xs text-muted">{{ view?.public_ip || "本机地址" }}</span>
            <span class="flex items-center justify-center text-xs text-muted"><span class="tnum">—</span></span>
            <span class="flex items-center justify-end gap-1 text-muted">
              <span class="inline-flex h-10 w-10 items-center justify-center sm:h-8 sm:w-8" aria-hidden="true"><ChevronRight :size="15" /></span>
            </span>
          </button>

          <button
            v-for="exit in rows"
            :key="exit.slot"
            type="button"
            class="grid w-full grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)_72px] items-center gap-4 border-b border-line px-4 py-3 text-left last:border-b-0 hover:bg-surface-2"
            @click="openExit(exit)"
          >
            <div class="flex min-w-0 items-center gap-2.5">
              <span class="min-w-8 rounded border border-line-strong bg-surface-2 px-1.5 py-0.5 text-center text-[10px] font-bold text-muted">{{ countryTag(exit) }}</span>
              <div class="min-w-0">
                <div class="truncate text-[13px] font-medium">{{ exitName(exit) }}</div>
                <div class="truncate text-[11px] text-faint">{{ exitSubLabel(exit) }} · {{ pad2(exit.slot) }}</div>
              </div>
            </div>
            <div class="space-y-1">
              <StatusBadge :label="exitStatusLabel(exit.status)" :tone="exitStatusTone(exit.status)" size="sm" :pulse="exit.status === 'starting'" />
              <span v-if="exit.pending_swap" class="block text-[10px] text-warn">排队换节点中</span>
              <span v-if="retryText(exit)" class="block text-[10px] text-muted">{{ retryText(exit) }}</span>
              <span v-if="checkItemState(exit)" class="flex items-center gap-1 text-[10px] text-info">
                <Spinner v-if="checkItemState(exit)?.running" :size="10" />
                {{ checkItemState(exit)?.label }}
              </span>
            </div>
            <div class="min-w-0 text-xs text-muted">
              <div class="truncate">{{ exit.exit_ip || "—" }}</div>
              <div v-if="lastExitText(exit)" class="truncate text-[10px] text-faint">{{ lastExitText(exit) }}</div>
            </div>
            <span class="flex items-center justify-center gap-1 text-xs text-muted"><span class="tnum">{{ formatLatency(exit.latency_ms) }}</span><LatencyTrend :trend="exit.latency_trend" /></span>
            <span class="flex items-center justify-end gap-1">
              <UiButton size="sm" variant="ghost" aria-label="查看详情" class="h-10 w-10 sm:h-8 sm:w-8" @click.stop="openExit(exit)"><ChevronRight :size="15" /></UiButton>
            </span>
          </button>
        </div>
        </div>
      </div>
    </Panel>

    <ExitTargetDialog
      v-model:open="targetOpen"
      :summary="summary"
      :allocated="allocated"
      :country-options="countryOptions"
    />

    <DialogShell
      :open="directEditOpen"
      title="本机网络订阅名称"
      description="自定义客户端订阅里显示的节点名；保存后重新更新订阅即可看到。"
      size="md"
      @update:open="directEditOpen = $event"
    >
      <form class="space-y-4" @submit.prevent="directNameMutation.mutate()">
        <label class="block">
          <span class="mb-1 block text-xs font-medium">名称方式</span>
          <select v-model="directForm.mode" name="direct-name-mode" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent">
            <option value="auto">默认名称</option>
            <option value="custom">自定义名称</option>
          </select>
        </label>
        <label v-if="directForm.mode === 'custom'" class="block">
          <span class="mb-1 block text-xs font-medium">自定义名称</span>
          <input v-model="directForm.remark" name="direct-name" maxlength="64" placeholder="例如：香港 · 直连" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
        </label>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="directEditOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="directNameMutation.isPending.value">保存名称</UiButton>
        </div>
      </form>
    </DialogShell>

    <ExitDetailDrawer
      :open="drawerOpen"
      :exit="selectedExit"
      :country-options="countryOptions"
      :check-running="checkRunning"
      @update:open="setDrawer"
    />
  </div>
</template>
