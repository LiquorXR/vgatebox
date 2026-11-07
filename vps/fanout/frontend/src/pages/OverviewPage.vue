<script setup lang="ts">
import { ArrowRight, Check, Copy, Database, Gauge, Link2, RefreshCw, Server, X } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { api, post } from "@/api/client";
import { keys, useConsoleQuery, useExitsQuery } from "@/api/queries";
import type { Exit, SubInfo } from "@/api/types";
import EmptyState from "@/components/EmptyState.vue";
import LatencyTrend from "@/components/LatencyTrend.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatCard from "@/components/StatCard.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { copyText } from "@/lib/clipboard";
import { formatBytes, formatDateTime, formatDuration, formatLatency, formatNumber, pad2 } from "@/lib/format";
import { buildSubUrl } from "@/lib/subscription";
import { toast, toastError } from "@/lib/toast";
import {
  countryTag,
  directEnabled,
  directPorts,
  errorLabel,
  exitName,
  exitStatusLabel,
  exitStatusTone,
  exitSubLabel,
} from "@/lib/status";

const router = useRouter();
const queryClient = useQueryClient();
const { data: summary, isLoading, isError, error } = useConsoleQuery();
const { data: exitsView, isLoading: exitsLoading } = useExitsQuery(true);

const s = computed(() => summary.value);
const exits = computed(() => exitsView.value?.exits || []);
const hostDirect = computed(() => exitsView.value?.host_direct || []);
const directOn = computed(() => directEnabled(hostDirect.value));
const target = computed(() => s.value?.target || 0);
const ready = computed(() => s.value?.ready || 0);
const waiting = computed(() => s.value?.waiting || 0);
const candidates = computed(() => s.value?.candidates || 0);
const progress = computed(() => (target.value > 0 ? (ready.value / target.value) * 100 : 0));
const memory = computed(() => {
  const value = s.value?.memory || {};
  const total = Number(value.MemTotal || 0);
  const available = Number(value.MemAvailable || 0);
  const used = Math.max(0, total - available);
  return { total, used, percent: total ? Math.round((used / total) * 100) : 0 };
});
const refreshRunning = computed(() => s.value?.refresh?.status === "running");
const checkRunning = computed(() => s.value?.check?.status === "running");
const check = computed(() => s.value?.check);
const refreshPercent = computed(() => {
  const r = s.value?.refresh;
  if (!r || r.status !== "running") return 0;
  return Math.min(100, ((r.elapsed_seconds || 0) / (r.timeout_seconds || 90)) * 100);
});
const checkPercent = computed(() => {
  const value = check.value;
  if (!value || !value.total) return 0;
  return (value.done / value.total) * 100;
});

const kicker = computed(() => {
  if (isError.value) return { text: "连接中断，正在重试", tone: "bad" as const };
  if (!s.value) return { text: "正在读取运行状态", tone: "neutral" as const };
  if (s.value.plan_error) return { text: errorLabel(s.value.plan_error) || "出口计划等待重试", tone: "warn" as const };
  if (!candidates.value) return { text: "尚未取得候选清单", tone: "warn" as const };
  if (waiting.value > 0) return { text: `${waiting.value} 个出口等待候选`, tone: "warn" as const };
  return { text: "所有出口运行正常", tone: "ok" as const };
});

const steps = computed(() => [
  {
    key: "candidates",
    title: "获取候选清单",
    hint: candidates.value ? `${formatNumber(candidates.value)} 个候选 · ${formatDateTime(s.value?.pool_generated_at)}` : "刷新来源后开始检测",
    done: candidates.value > 0,
    active: candidates.value === 0,
  },
  {
    key: "exits",
    title: "创建并等待出口就绪",
    hint: ready.value ? `${ready.value} 个就绪，${waiting.value} 个等待` : "取得候选后自动创建出口",
    done: ready.value > 0,
    active: candidates.value > 0 && ready.value === 0,
  },
  {
    key: "subscribe",
    title: "复制订阅并导入客户端",
    hint: ready.value ? "v2rayN 或 Clash Verge" : "出口就绪后可用",
    done: false,
    active: ready.value > 0,
  },
]);

const recentExits = computed(() => exits.value.slice(0, 6));

const refreshMutation = useMutation({
  mutationFn: () => post<{ status: string }>("api/console/refresh", {}),
  onSuccess: () => {
    toast("正在刷新来源，完成后会自动更新列表");
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "刷新失败"),
});

const cancelCheckMutation = useMutation({
  mutationFn: () => post("api/console/check/cancel", {}),
  onSuccess: () => toast("正在取消检测并清理临时连接"),
  onError: (error) => toastError(error, "取消失败"),
});

const directMutation = useMutation({
  mutationFn: (enabled: boolean) => post("api/console/direct", { enabled }),
  onSuccess: (_, enabled) => {
    toast(enabled ? "已启用 VPS 本机直出" : "已停用 VPS 本机直出", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "切换直出失败"),
});

async function copySubscription() {
  try {
    const sub = await queryClient.fetchQuery({
      queryKey: keys.sub,
      queryFn: () => api<SubInfo>("api/sub"),
      staleTime: 60_000,
    });
    const url = buildSubUrl(sub, "base64", {
      vpnAll: true,
      slots: new Set(),
      direct: true,
      thirdAll: true,
      proxies: new Set(),
    });
    await copyText(url);
    toast("订阅链接已复制", { tone: "success" });
  } catch (error) {
    toastError(error, "复制订阅失败");
  }
}

function openExit(exit: Exit) {
  void router.push({ path: "/exits", query: { slot: String(exit.slot) } });
}
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="总览" description="来源、出口、订阅，一条链路看清。" :kicker="kicker.text" :tone="kicker.tone">
      <template #actions>
        <UiButton :loading="refreshMutation.isPending.value || refreshRunning" @click="refreshMutation.mutate()">
          <RefreshCw :size="15" />
          刷新来源
        </UiButton>
        <UiButton variant="primary" :disabled="ready === 0" @click="copySubscription">
          <Copy :size="15" />
          复制订阅
        </UiButton>
      </template>
    </PageHeader>

    <div
      v-if="isError"
      class="rounded-xl border border-bad/25 bg-bad-soft px-4 py-3 text-sm text-bad"
      role="alert"
    >
      无法读取运行状态：{{ error?.message || "请检查网络后重试" }}
      <button class="ml-2 font-semibold underline" type="button" @click="queryClient.invalidateQueries({ queryKey: keys.console })">
        重试
      </button>
    </div>

    <div v-if="s" class="grid grid-cols-2 gap-2 rounded-xl border border-line bg-surface p-2.5 sm:grid-cols-[repeat(4,minmax(0,1fr))] sm:items-center">
      <RouterLink to="/nodes" class="min-w-0 rounded-lg px-2.5 py-2 transition-colors hover:bg-surface-2">
        <div class="flex items-center gap-1.5 text-[11px] text-muted"><Database :size="13" /> 候选节点</div>
        <div class="tnum mt-1 text-xl font-semibold">{{ formatNumber(candidates) }}</div>
        <div class="truncate text-[11px] text-faint">{{ formatDateTime(s.pool_generated_at) }}</div>
      </RouterLink>
      <RouterLink to="/exits" class="min-w-0 rounded-lg bg-accent-soft px-2.5 py-2 transition-colors hover:bg-accent-soft/80">
        <div class="flex items-center gap-1.5 text-[11px] text-accent"><Server :size="13" /> 就绪出口</div>
        <div class="tnum mt-1 text-xl font-semibold text-accent">{{ ready }}<span class="text-xs font-medium text-muted"> / {{ target }}</span></div>
        <div class="truncate text-[11px] text-muted">{{ waiting }} 个等待</div>
      </RouterLink>
      <RouterLink to="/subscription" class="min-w-0 rounded-lg px-2.5 py-2 transition-colors hover:bg-surface-2">
        <div class="flex items-center gap-1.5 text-[11px] text-muted"><Link2 :size="13" /> 订阅入口</div>
        <div class="tnum mt-1 text-xl font-semibold">{{ ready + (directOn ? 1 : 0) }}</div>
        <div class="truncate text-[11px] text-faint">VPN Gate + 本机直出 · 2 种格式</div>
      </RouterLink>
      <RouterLink to="/settings" class="min-w-0 rounded-lg px-2.5 py-2 transition-colors hover:bg-surface-2">
        <div class="flex items-center gap-1.5 text-[11px] text-muted"><Gauge :size="13" /> 主机内存</div>
        <div class="tnum mt-1 text-xl font-semibold">{{ memory.percent }}<span class="text-xs font-medium text-muted">%</span></div>
        <div class="truncate text-[11px] text-faint">{{ formatBytes(memory.used) }} / {{ formatBytes(memory.total) }}</div>
      </RouterLink>
    </div>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1.65fr)_minmax(0,1fr)]">
      <div class="min-w-0 space-y-4">
        <Panel title="出口健康" description="按本机直出、自定义代理、VPN Gate 排序">
          <template #actions>
            <RouterLink to="/exits" class="text-xs font-semibold text-accent hover:underline">管理全部 →</RouterLink>
          </template>

          <div v-if="exitsLoading && !exitsView" class="space-y-3">
            <SkeletonBlock :lines="5" />
          </div>
          <div v-else-if="!recentExits.length && !directOn" class="py-4">
            <EmptyState compact title="还没有可用的出口" description="先刷新来源，取得候选后系统会按目标数量创建出口。">
              <template #icon><Server :size="26" /></template>
              <template #action>
                <UiButton @click="router.push('/nodes')">前往节点池</UiButton>
              </template>
            </EmptyState>
          </div>
          <div v-else class="-mx-4 -mb-4">
            <div class="min-w-0">
              <div class="grid grid-cols-[minmax(0,1.4fr)_auto_auto] gap-3 border-b border-line px-4 py-2 text-[11px] text-faint sm:grid-cols-[minmax(0,1.6fr)_0.8fr_0.55fr_auto]">
                <span>出口</span><span>状态</span><span class="hidden sm:block">延迟</span><span></span>
              </div>

              <button
                v-if="directOn"
                type="button"
                class="grid w-full grid-cols-[minmax(0,1.4fr)_auto_auto] items-center gap-3 border-b border-line px-4 py-2.5 text-left hover:bg-surface-2 sm:grid-cols-[minmax(0,1.6fr)_0.8fr_0.55fr_auto]"
                @click="router.push('/exits')"
              >
                <span class="flex min-w-0 items-center gap-2.5">
                  <span class="rounded border border-accent bg-accent-soft px-1.5 py-0.5 text-[10px] font-bold text-accent">VPS</span>
                  <span class="min-w-0">
                    <span class="block truncate text-[13px] font-medium">本机直出</span>
                    <span class="block truncate text-[11px] text-faint">本机网络 · 端口 {{ directPorts(hostDirect).join(" / ") || "—" }}</span>
                  </span>
                </span>
                <StatusBadge label="已启用" tone="ok" size="sm" class="justify-self-start" />
                <span class="tnum hidden text-xs text-muted sm:block">—</span>
                <ArrowRight :size="14" class="text-faint" />
              </button>

              <button
                v-for="exit in recentExits"
                :key="exit.slot"
                type="button"
                class="grid w-full grid-cols-[minmax(0,1.4fr)_auto_auto] items-center gap-3 border-b border-line px-4 py-2.5 text-left last:border-b-0 hover:bg-surface-2 sm:grid-cols-[minmax(0,1.6fr)_0.8fr_0.55fr_auto]"
                @click="openExit(exit)"
              >
                <span class="flex min-w-0 items-center gap-2.5">
                  <span class="min-w-8 rounded border border-line-strong bg-surface-2 px-1.5 py-0.5 text-center text-[10px] font-bold text-muted">{{ countryTag(exit) }}</span>
                  <span class="min-w-0">
                    <span class="block truncate text-[13px] font-medium">{{ exitName(exit) }}</span>
                    <span class="block truncate text-[11px] text-faint">{{ exitSubLabel(exit) }}</span>
                  </span>
                </span>
                <StatusBadge :label="exitStatusLabel(exit.status)" :tone="exitStatusTone(exit.status)" size="sm" :pulse="exit.status === 'starting'" class="justify-self-start" />
                <span class="hidden items-center gap-1 text-xs text-muted sm:flex"><span class="tnum">{{ formatLatency(exit.latency_ms) }}</span><LatencyTrend :trend="exit.latency_trend" /></span>
                <ArrowRight :size="14" class="text-faint" />
              </button>
            </div>
          </div>
        </Panel>

        <div class="grid gap-4 sm:grid-cols-2">
          <Panel title="VPS 本机直出" description="使用服务器公网 IP，独立加入订阅；不会成为其他出口的故障备用。">
            <div class="flex items-center justify-between gap-4">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <StatusBadge :label="directOn ? '已启用' : '未启用'" :tone="directOn ? 'ok' : 'neutral'" size="sm" />
                  <span v-if="directOn" class="truncate text-xs text-faint">端口 {{ directPorts(hostDirect).join(" / ") || "—" }}</span>
                </div>
                <p class="mt-2 text-xs text-muted">未配置 VPN Gate 时，可先用它验证客户端接入。</p>
              </div>
              <ToggleSwitch
                :model-value="directOn"
                label="启用或停用本机直出"
                :disabled="directMutation.isPending.value"
                @update:model-value="directMutation.mutate($event)"
              />
            </div>
          </Panel>

          <Panel title="推荐步骤" description="按当前状态自动推进">
            <ol class="space-y-2.5">
              <li v-for="(step, index) in steps" :key="step.key" class="flex items-start gap-2.5">
                <span
                  class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border text-[10px] font-bold"
                  :class="
                    step.done
                      ? 'border-transparent bg-ok-soft text-ok'
                      : step.active
                        ? 'border-accent bg-accent text-accent-ink'
                        : 'border-line-strong text-faint'
                  "
                >
                  <Check v-if="step.done" :size="12" />
                  <template v-else>{{ index + 1 }}</template>
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block text-[13px] font-medium" :class="step.done ? 'text-muted' : 'text-ink'">{{ step.title }}</span>
                  <span class="block truncate text-[11px] text-faint">{{ step.hint }}</span>
                </span>
                <RouterLink
                  v-if="step.key === 'subscribe' && step.active"
                  to="/subscription"
                  class="shrink-0 text-[11px] font-semibold text-accent hover:underline"
                >
                  去订阅 →
                </RouterLink>
              </li>
            </ol>
          </Panel>
        </div>
      </div>

      <div class="min-w-0 space-y-4">
        <Panel title="运行中的任务">
          <div v-if="checkRunning && check" class="space-y-3">
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="font-medium text-ink">候选连通性检测</span>
              <span class="tnum text-muted">{{ check.done }} / {{ check.total }}</span>
            </div>
            <ProgressBar :value="checkPercent" label="检测进度" />
            <div class="flex items-center justify-between gap-2 text-[11px] text-faint">
              <span>并发 {{ check.parallelism }}</span>
              <UiButton variant="ghost" size="sm" :loading="cancelCheckMutation.isPending.value" @click="cancelCheckMutation.mutate()">
                <X :size="13" />取消
              </UiButton>
            </div>
          </div>
          <div v-else-if="refreshRunning && s" class="space-y-3">
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="font-medium text-ink">刷新候选来源</span>
              <span class="tnum text-muted">{{ formatDuration(s.refresh.elapsed_seconds) }}</span>
            </div>
            <ProgressBar :value="refreshPercent" tone="info" label="刷新进度" />
            <p class="text-[11px] text-faint">最长等待 {{ s.refresh.timeout_seconds || 90 }} 秒；失败会保留旧清单。</p>
          </div>
          <div v-else class="space-y-3">
            <div class="flex items-center gap-2">
              <span class="grid size-8 place-items-center rounded-lg bg-ok-soft text-ok"><Check :size="16" /></span>
              <div>
                <p class="text-[13px] font-medium">当前没有运行中的任务</p>
                <p class="text-[11px] text-faint">上次刷新 {{ formatDateTime(s?.refresh?.checked_at) }}</p>
              </div>
            </div>
            <RouterLink to="/nodes" class="text-[11px] font-semibold text-accent hover:underline">前往节点池 →</RouterLink>
          </div>
        </Panel>

        <Panel title="主机资源">
          <div class="space-y-3">
            <div class="flex items-center justify-between text-xs">
              <span class="flex items-center gap-1.5 text-muted"><Gauge :size="14" />内存使用</span>
              <span class="tnum font-semibold">{{ memory.percent }}%</span>
            </div>
            <ProgressBar :value="memory.percent" :tone="memory.percent > 85 ? 'bad' : memory.percent > 70 ? 'warn' : 'accent'" label="内存使用" />
            <p class="text-[11px] text-faint">增加出口会占用更多资源；软件数量上限不代表主机承载能力。</p>
          </div>
        </Panel>

      </div>
    </div>

    <Panel v-if="isLoading && !s" title="正在读取控制台">
      <SkeletonBlock :lines="6" />
    </Panel>
  </div>
</template>
