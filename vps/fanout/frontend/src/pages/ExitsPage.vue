<script setup lang="ts">
import { ArrowLeftRight, ChevronRight, Pause, Play, RefreshCw, RotateCcw, Search, ShieldCheck, SlidersHorizontal, X } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { post, queryString } from "@/api/client";
import { keys, useConsoleQuery, useExitsQuery, useNodesQuery } from "@/api/queries";
import type { Exit, ExitInbound, ExitPlan, SlotPolicy } from "@/api/types";
import DialogShell from "@/components/DialogShell.vue";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import EmptyState from "@/components/EmptyState.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SectionTabs from "@/components/SectionTabs.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import Spinner from "@/components/Spinner.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { countryName, formatDateTime, formatLatency, pad2 } from "@/lib/format";
import { initialTarget } from "@/lib/plan";
import { toast, toastError } from "@/lib/toast";
import {
  countryTag,
  directEnabled,
  directPorts,
  exitCheck,
  exitKind,
  exitName,
  exitStatusLabel,
  exitStatusTone,
  exitSubLabel,
  ipTypeLabels,
  phaseLabels,
} from "@/lib/status";

const ALL_TYPES = Object.keys(ipTypeLabels);

function clonePolicy(value: SlotPolicy): SlotPolicy {
  return JSON.parse(JSON.stringify(value)) as SlotPolicy;
}

function defaultPolicy(country = ""): SlotPolicy {
  return {
    country,
    allowed_ip_types: [...ALL_TYPES],
    preferred_isp: [],
    allowed_isp: [],
    blocked_isp: [],
    allowed_asn: [],
    blocked_asn: [],
    maximum_latency_ms: 0,
  };
}

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
  { key: "direct", label: "本机直出", count: directOn.value ? 1 : 0 },
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
    return [exitName(exit), exit.host, exit.exit_ip, exit.country, exit.region, String(exit.slot)]
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

// Target dialog
const targetOpen = ref(false);
const targetCount = ref(16);
const targetCountry = ref("");
const targetServer = ref("www.tesla.com");
const confirmTargetOpen = ref(false);
const pendingSave = ref<(() => Promise<void>) | null>(null);

function openTarget() {
  const s = summary.value;
  if (!s) {
    toast("请等待状态读取完成");
    return;
  }
  targetCount.value = initialTarget(s);
  targetCountry.value = s.plan?.policy?.country || "";
  targetServer.value = s.plan?.server_name || s.default_server_name || "www.tesla.com";
  targetOpen.value = true;
}

const targetMutation = useMutation({
  mutationFn: async () => {
    const count = Math.max(0, Math.trunc(Number(targetCount.value)));
    if (!Number.isFinite(count) || count < 0 || count > (summary.value?.capacity || 0)) {
      throw new Error(`出口数量须为 0–${summary.value?.capacity || 0}`);
    }
    const base = summary.value?.plan?.policy;
    const policy: SlotPolicy = base ? clonePolicy(base) : defaultPolicy();
    policy.country = targetCountry.value.trim().toUpperCase();
    const serverName = targetServer.value.trim().toLowerCase();
    if (!serverName || !serverName.includes(".")) throw new Error("REALITY 目标须为有效域名");
    const body: ExitPlan = { count, policy, server_name: serverName };
    return post("api/console/target", body);
  },
  onSuccess: () => {
    toast("目标数量已保存，正在按策略连接", { tone: "success" });
    targetOpen.value = false;
    confirmTargetOpen.value = false;
    void queryClient.invalidateQueries({ queryKey: keys.console });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "保存出口数量失败"),
});

function submitTarget() {
  const count = Math.max(0, Math.trunc(Number(targetCount.value)));
  if (count < allocated.value) {
    pendingSave.value = async () => {
      await targetMutation.mutateAsync();
    };
    confirmTargetOpen.value = true;
    return;
  }
  targetMutation.mutate();
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

const swapConfirmOpen = ref(false);
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

// Drawer
const drawerOpen = ref(false);
const activeSlot = ref(0);
const selectedExit = computed(() => exits.value.find((exit) => exit.slot === activeSlot.value) || null);

const nameEdits = reactive<Record<number, { mode: string; remark: string }>>({});
const policyForm = reactive<SlotPolicy>(defaultPolicy());

const countryOptions = computed(() => {
  const map = new Map<string, string>();
  for (const node of nodesView.value?.nodes || []) {
    if (node.country_code) map.set(node.country_code, node.country || countryName(node.country_code));
  }
  for (const exit of exits.value) {
    if (exit.region) map.set(exit.region, countryName(exit.region));
  }
  if (policyForm.country) map.set(policyForm.country, countryName(policyForm.country));
  return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
});

function populateDrawer(exit: Exit | null) {
  for (const key of Object.keys(nameEdits)) delete nameEdits[Number(key)];
  for (const inbound of exit?.inbounds || []) {
    nameEdits[inbound.id] = { mode: inbound.name_mode === "custom" ? "custom" : "auto", remark: inbound.remark || "" };
  }
  Object.assign(policyForm, exit?.policy ? clonePolicy(exit.policy) : defaultPolicy());
}

watch(
  () => selectedExit.value?.slot,
  (slot) => {
    if (slot) populateDrawer(selectedExit.value);
  },
  { immediate: true },
);

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

const statusOptions = [
  { value: "all", label: "全部状态" },
  { value: "up", label: "已就绪" },
  { value: "starting", label: "连接中" },
  { value: "waiting", label: "等待候选" },
  { value: "stopped", label: "已停止" },
  { value: "blocked", label: "已阻断" },
  { value: "error", label: "连接异常" },
];
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="出口" description="统一查看本机直出、自定义代理和 VPN Gate，明确每条连接的去向。">
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
        <input v-model="search" name="exit-search" type="search" placeholder="搜索名称、IP 或国家" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
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
          <div class="min-w-[720px]">
          <div class="grid grid-cols-[minmax(0,1.5fr)_0.7fr_0.7fr_0.7fr_auto] gap-3 border-b border-line px-4 py-2 text-[11px] text-faint">
            <span>出口</span><span>状态</span><span>实际出口</span><span>延迟</span><span class="text-right">操作</span>
          </div>

          <div v-if="showDirect" class="grid grid-cols-[minmax(0,1.5fr)_0.7fr_0.7fr_0.7fr_auto] items-center gap-3 border-b border-line px-4 py-3">
            <div class="flex min-w-0 items-center gap-2.5">
              <span class="rounded border border-accent bg-accent-soft px-1.5 py-0.5 text-[10px] font-bold text-accent">VPS</span>
              <div class="min-w-0">
                <div class="truncate text-[13px] font-medium">本机直出</div>
                <div class="truncate text-[11px] text-faint">本机网络 · 端口 {{ directPorts(hostDirect).join(" / ") || "—" }}</div>
              </div>
            </div>
            <StatusBadge label="已启用" tone="ok" size="sm" />
            <span class="truncate text-xs text-muted">{{ view?.public_ip || "本机地址" }}</span>
            <span class="tnum text-xs text-muted">—</span>
            <span class="text-right text-[11px] text-faint">在总览管理</span>
          </div>

          <button
            v-for="exit in rows"
            :key="exit.slot"
            type="button"
            class="grid w-full grid-cols-[minmax(0,1.5fr)_0.7fr_0.7fr_0.7fr_auto] items-center gap-3 border-b border-line px-4 py-3 text-left last:border-b-0 hover:bg-surface-2"
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
              <span v-if="checkItemState(exit)" class="flex items-center gap-1 text-[10px] text-info">
                <Spinner v-if="checkItemState(exit)?.running" :size="10" />
                {{ checkItemState(exit)?.label }}
              </span>
            </div>
            <span class="truncate text-xs text-muted">{{ exit.exit_ip || "—" }}</span>
            <span class="tnum text-xs text-muted">{{ formatLatency(exit.latency_ms) }}</span>
            <span class="flex items-center justify-end gap-1">
              <UiButton size="sm" variant="ghost" aria-label="查看详情" @click.stop="openExit(exit)"><ChevronRight :size="15" /></UiButton>
            </span>
          </button>
        </div>
        </div>
      </div>
    </Panel>

    <DialogShell
      :open="targetOpen"
      title="调整 VPN Gate 数量"
      description="按服务器资源和上游供给设置目标；候选不足时会保留策略等待。"
      size="md"
      @update:open="targetOpen = $event"
    >
      <form class="space-y-4" @submit.prevent="submitTarget">
        <label class="block">
          <span class="mb-1 block text-xs font-medium text-ink">目标数量</span>
          <input v-model.number="targetCount" name="target-count" type="number" min="0" :max="summary?.capacity || 0" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          <span class="mt-1 block text-[11px] text-faint">当前已分配 {{ allocated }} 个；设为 0 会移除全部活动出口。</span>
        </label>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block">
            <span class="mb-1 block text-xs font-medium text-ink">新增出口国家代码</span>
            <input v-model="targetCountry" name="target-country" maxlength="2" placeholder="例如 JP、KR、US" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm uppercase outline-none focus:border-accent" />
            <span class="mt-1 block text-[11px] text-faint">留空自动选择。</span>
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium text-ink">REALITY 目标域名</span>
            <input v-model="targetServer" name="target-server" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
            <span class="mt-1 block text-[11px] text-faint">默认 {{ summary?.default_server_name || "www.tesla.com" }}。</span>
          </label>
        </div>
        <div v-if="targetCount < allocated" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-xs leading-relaxed text-warn">
          目标数量低于当前分配数，将从编号最大的出口开始移除，相关连接会中断。
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="targetOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="targetMutation.isPending.value">保存并应用</UiButton>
        </div>
      </form>
    </DialogShell>

    <ConfirmDialog
      :open="confirmTargetOpen"
      title="确认减少出口数量"
      :description="`将从编号最大的出口开始移除，目标数量为 ${targetCount} 个；正在使用的连接会中断。`"
      confirm-label="确认移除"
      :busy="targetMutation.isPending.value"
      @update:open="confirmTargetOpen = $event"
      @confirm="pendingSave?.()"
    />

    <ConfirmDialog
      :open="swapConfirmOpen"
      title="更换出口 IP"
      description="将断开当前节点并重新挑选一个候选；出口编号、端口和订阅入口保持不变。正在使用该出口的客户端会短暂中断。"
      confirm-label="更换 IP"
      :busy="swapMutation.isPending.value"
      @update:open="swapConfirmOpen = $event"
      @confirm="selectedExit && swapMutation.mutate(selectedExit.slot)"
    />

    <DialogShell
      :open="drawerOpen"
      :title="selectedExit ? exitName(selectedExit) : '出口详情'"
      :description="selectedExit ? exitSubLabel(selectedExit) : undefined"
      variant="drawer"
      @update:open="setDrawer"
    >
      <div v-if="!selectedExit" class="text-sm text-muted">出口不存在，可能已被移除。</div>
      <div v-else class="space-y-5">
        <div class="flex flex-wrap items-center gap-2">
          <StatusBadge :label="exitStatusLabel(selectedExit.status)" :tone="exitStatusTone(selectedExit.status)" :pulse="selectedExit.status === 'starting'" />
          <span v-if="selectedExit.phase" class="text-[11px] text-faint">{{ selectedExit.phase }}</span>
          <span v-if="selectedExit.err" class="text-[11px] text-bad">{{ selectedExit.err }}</span>
        </div>

        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
          <div><dt class="text-faint">出口编号</dt><dd class="tnum mt-0.5 font-medium">{{ pad2(selectedExit.slot) }}</dd></div>
          <div><dt class="text-faint">延迟</dt><dd class="tnum mt-0.5 font-medium">{{ formatLatency(selectedExit.latency_ms) }}</dd></div>
          <div><dt class="text-faint">实际出口</dt><dd class="mt-0.5 truncate font-mono text-[11px]">{{ selectedExit.exit_ip || "—" }}</dd></div>
          <div><dt class="text-faint">来源地区</dt><dd class="mt-0.5">{{ countryName(selectedExit.region) }}（{{ selectedExit.region || "??" }}）</dd></div>
          <div><dt class="text-faint">启动于</dt><dd class="mt-0.5">{{ formatDateTime(selectedExit.since) }}</dd></div>
          <div><dt class="text-faint">接入端口</dt><dd class="tnum mt-0.5">{{ selectedExit.port || "—" }}</dd></div>
        </dl>

        <div class="flex flex-wrap gap-2">
          <UiButton size="sm" :loading="checkMutation.isPending.value" :disabled="checkRunning" @click="checkMutation.mutate(selectedExit.slot)">
            <RefreshCw :size="14" />立即检测
          </UiButton>
          <UiButton size="sm" :loading="swapMutation.isPending.value" @click="swapConfirmOpen = true">
            <ArrowLeftRight :size="14" />更换 IP
          </UiButton>
          <UiButton
            v-if="selectedExit.status === 'stopped'"
            size="sm"
            :loading="pauseMutation.isPending.value"
            @click="pauseMutation.mutate({ slot: selectedExit.slot, resume: true })"
          >
            <Play :size="14" />恢复
          </UiButton>
          <UiButton
            v-else
            size="sm"
            :loading="pauseMutation.isPending.value"
            @click="pauseMutation.mutate({ slot: selectedExit.slot, resume: false })"
          >
            <Pause :size="14" />暂停
          </UiButton>
          <UiButton
            v-if="selectedExit.can_rollback_policy"
            size="sm"
            :loading="rollbackMutation.isPending.value"
            @click="rollbackMutation.mutate(selectedExit.slot)"
          >
            <RotateCcw :size="14" />回滚策略
          </UiButton>
        </div>

        <section>
          <h3 class="mb-2 text-xs font-semibold text-ink">订阅名称</h3>
          <div v-if="selectedExit.inbounds?.length" class="space-y-2">
            <div v-for="inbound in selectedExit.inbounds" :key="inbound.id" class="rounded-lg border border-line p-3">
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
          <form class="space-y-3" @submit.prevent="policyMutation.mutate(selectedExit.slot)">
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="block">
                <span class="mb-1 block text-[11px] text-faint">指定国家</span>
                <select v-model="policyForm.country" name="policy-country" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent">
                  <option value="">留空自动</option>
                  <option v-for="[code, label] in countryOptions" :key="code" :value="code">{{ label }}（{{ code }}）</option>
                </select>
              </label>
              <label class="block">
                <span class="mb-1 block text-[11px] text-faint">延迟上限（毫秒，0 不限）</span>
                <input v-model.number="policyForm.maximum_latency_ms" name="policy-latency" type="number" min="0" max="120000" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 text-xs outline-none focus:border-accent" />
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
  </div>
</template>
