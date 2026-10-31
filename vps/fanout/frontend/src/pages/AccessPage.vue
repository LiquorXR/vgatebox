<script setup lang="ts">
import { Copy, Download, Globe2, Plug, Plus, Search, ShieldCheck, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { z } from "zod";
import { api, ApiError, apiUrl, post, queryString } from "@/api/client";
import { keys, useExitsQuery, useSocksLinksQuery, useSstpQuery, useThirdPartyQuery } from "@/api/queries";
import type { Exit, ThirdPartyProxy, ThirdPartyRow } from "@/api/types";
import AdvancedAccessPanel from "@/components/AdvancedAccessPanel.vue";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import CopyButton from "@/components/CopyButton.vue";
import DialogShell from "@/components/DialogShell.vue";
import EmptyState from "@/components/EmptyState.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SectionTabs from "@/components/SectionTabs.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { copyText } from "@/lib/clipboard";
import { downloadText } from "@/lib/download";
import { countryName, formatDateTime, formatLatency, truncate } from "@/lib/format";
import { checkStatusLabel, checkStatusTone, errorLabel, exitName, exitStatusLabel, exitStatusTone } from "@/lib/status";

import { toast, toastError } from "@/lib/toast";

const queryClient = useQueryClient();
const { data: exitsView, isLoading: exitsLoading } = useExitsQuery(true);
const tab = ref("socks");
const native = computed(() => exitsView.value?.backend === "native");
const exits = computed(() => exitsView.value?.exits || []);
const hostDirect = computed(() => exitsView.value?.host_direct || []);
const direct = computed(() => exitsView.value?.direct || []);
const inboundCount = computed(
  () =>
    exits.value.reduce((total, exit) => total + (exit.inbounds?.length || 0), 0) +
    hostDirect.value.length +
    direct.value.length,
);

const { data: socksData } = useSocksLinksQuery(computed(() => tab.value === "socks"));
const { data: sstpData, isLoading: sstpLoading } = useSstpQuery(computed(() => tab.value === "sstp"));
const { data: thirdData, isLoading: thirdLoading } = useThirdPartyQuery(
  computed(() => tab.value === "third" && native.value),
);

const tabs = computed(() => [
  { key: "socks", label: "VPS SOCKS5", count: exits.value.filter((exit) => exit.public_socks?.enable).length },
  { key: "sstp", label: "原始 SSTP", count: sstpData.value?.nodes.length },
  { key: "third", label: "第三方 SOCKS5", count: thirdData.value?.proxies.length },
  { key: "advanced", label: "高级接入", count: inboundCount.value },
]);

// ---- VPS SOCKS5 ----
const socksRows = computed(() => {
  const links = new Map((socksData.value?.links || []).map((link) => [link.slot, link]));
  return exits.value.map((exit) => ({
    exit,
    link: links.get(exit.slot),
    enabled: Boolean(exit.public_socks?.enable),
  }));
});

const socksDetailOpen = ref(false);
const socksDetailSlot = ref(0);
const socksUser = ref("");
const socksPass = ref("");
const socksDetail = computed(() => socksRows.value.find((row) => row.exit.slot === socksDetailSlot.value) || null);

function socksAddress(exit: Exit, external: boolean): string {
  const port = external ? exit.public_socks?.port : exit.port;
  if (!port) return "";
  const host = external ? exitsView.value?.public_ip || window.location.hostname : "127.0.0.1";
  const auth = exit.socks_user
    ? `${encodeURIComponent(exit.socks_user)}:${encodeURIComponent(exit.socks_pass || "")}@`
    : "";
  return `socks5://${auth}${host}:${port}`;
}

function openSocksDetail(row: { exit: Exit }) {
  socksDetailSlot.value = row.exit.slot;
  socksUser.value = row.exit.socks_user || "";
  socksPass.value = row.exit.socks_pass || "";
  socksDetailOpen.value = true;
}

const socksCredMutation = useMutation({
  mutationFn: () => {
    if (!socksDetail.value) throw new Error("出口不存在");
    return post(
      `api/cred${queryString({
        slot: socksDetail.value.exit.slot,
        user: socksUser.value.trim(),
        pass: socksPass.value.trim(),
      })}`,
      {},
    );
  },
  onSuccess: (value) => {
    const result = value as { user: string; pass: string };
    socksUser.value = result.user;
    socksPass.value = result.pass;
    toast(socksUser.value || socksPass.value ? "SOCKS5 凭据已保存" : "SOCKS5 凭据已随机重置", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.socks });
  },
  onError: (error) => accessError(error, "保存 SOCKS5 凭据失败"),
});

function accessError(error: unknown, fallback: string) {
  void queryClient.invalidateQueries({ queryKey: keys.exits });
  void queryClient.invalidateQueries({ queryKey: keys.socks });
  void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
  if (error instanceof ApiError && error.status >= 500) {
    toast(`操作未完成（HTTP ${error.status}）。已刷新当前状态，请确认是否生效后再重试。`, {
      tone: "error",
      duration: 9000,
    });
    return;
  }
  toastError(error, fallback);
}

const socksMutation = useMutation({
  mutationFn: ({ slot, enabled }: { slot: number; enabled: boolean }) => post("api/console/socks", { slot, enabled }),
  onSuccess: () => {
    toast("外部 SOCKS5 设置已更新", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.socks });
  },
  onError: (error) => accessError(error, "更新外部访问失败"),
});

async function copySocks(url: string) {
  try {
    await copyText(url);
    toast("SOCKS5 地址已复制", { tone: "success" });
  } catch (error) {
    toastError(error, "复制失败");
  }
}

async function copyAllSocks() {
  const urls = (socksData.value?.links || []).map((link) => link.url);
  if (!urls.length) {
    toast("没有已启用且就绪的 SOCKS5 连接", { tone: "error" });
    return;
  }
  await copySocks(urls.join("\n"));
}

// ---- SSTP ----
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

// ---- Third-party ----
const thirdRows = computed<ThirdPartyRow[]>(() => thirdData.value?.proxies || []);
const thirdCountries = computed(() => thirdData.value?.countries || []);
const thirdOpen = ref(false);
const thirdEditing = ref(0);
const thirdSaving = ref(false);
const thirdFormLoading = ref(false);
const thirdForm = reactive({
  host: "",
  port: 1080,
  username: "",
  password: "",
  country: "",
  city: "",
  remark: "",
  subscribe: true,
  enabled: true,
});
const thirdSchema = z.object({
  host: z.string().trim().min(1, "请填写服务器域名或 IP").max(253),
  port: z.coerce.number().int().min(1, "端口须为 1–65535").max(65535),
  username: z.string().trim().min(1, "请填写账号").max(255),
  password: z.string().max(255).optional(),
  country: z.string().trim().max(2).optional(),
  city: z.string().trim().max(100).optional(),
  remark: z.string().trim().max(160).optional(),
});

function resetThird() {
  Object.assign(thirdForm, {
    host: "",
    port: 1080,
    username: "",
    password: "",
    country: "",
    city: "",
    remark: "",
    subscribe: true,
    enabled: true,
  });
}

async function openThird(id = 0) {
  thirdEditing.value = id;
  resetThird();
  if (id) {
    thirdFormLoading.value = true;
    try {
      const value = await api<{ id: number; proxy: ThirdPartyProxy; enabled: boolean }>(
        `api/resources/third-party${queryString({ id })}`,
      );
      Object.assign(thirdForm, value.proxy, { password: "", enabled: value.enabled });
    } catch (error) {
      toastError(error, "读取代理失败");
      return;
    } finally {
      thirdFormLoading.value = false;
    }
  }
  thirdOpen.value = true;
}

async function saveThird() {
  const parsed = thirdSchema.safeParse({
    host: thirdForm.host,
    port: thirdForm.port,
    username: thirdForm.username,
    password: thirdForm.password,
    country: thirdForm.country,
    city: thirdForm.city,
    remark: thirdForm.remark,
  });
  if (!parsed.success) {
    toast(parsed.error.issues[0]?.message || "请检查表单", { tone: "error" });
    return;
  }
  if (!thirdEditing.value && !thirdForm.password) {
    toast("新增代理须填写密码", { tone: "error" });
    return;
  }
  const country = thirdForm.country.trim().toUpperCase();
  if (country && !/^[A-Z]{2}$/.test(country)) {
    toast("国家须为两位代码，如 US、JP；也可留空", { tone: "error" });
    return;
  }
  thirdSaving.value = true;
  try {
    await post("api/resources/third-party", {
      id: thirdEditing.value,
      host: thirdForm.host.trim(),
      port: Number(thirdForm.port),
      username: thirdForm.username.trim(),
      password: thirdForm.password,
      country,
      city: thirdForm.city.trim(),
      remark: thirdForm.remark.trim(),
      subscribe: thirdForm.subscribe,
      enabled: thirdForm.enabled,
    });
    thirdOpen.value = false;
    toast("代理已保存", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  } catch (error) {
    accessError(error, "保存代理失败");
  } finally {
    thirdSaving.value = false;
  }
}

async function toggleThird(row: ThirdPartyRow, action: "toggle" | "subscribe") {
  try {
    await post(`api/resources/third-party/${action}`, {
      id: row.id,
      ...(action === "toggle" ? { enabled: !row.enabled } : { subscribe: !row.subscribe }),
    });
    toast(action === "toggle" ? "启用状态已更新" : "订阅设置已更新", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  } catch (error) {
    accessError(error, "更新失败");
  }
}

const checkingThird = reactive(new Set<number>());
async function checkThird(row: ThirdPartyRow) {
  checkingThird.add(row.id);
  try {
    await post("api/resources/third-party/check", { id: row.id });
    toast("已提交代理检测");
    void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
  } catch (error) {
    toastError(error, "提交检测失败");
  } finally {
    checkingThird.delete(row.id);
  }
}

const deleteOpen = ref(false);
const deleteRow = ref<ThirdPartyRow | null>(null);
const deleteMutation = useMutation({
  mutationFn: (id: number) => post("api/resources/third-party/delete", { id }),
  onSuccess: () => {
    deleteOpen.value = false;
    toast("代理已删除", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
  },
  onError: (error) => toastError(error, "删除代理失败"),
});
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="接入" description="管理服务器代理与原始连接，各自有明确的用途。">
      <template #actions>
        <UiButton v-if="tab === 'socks'" @click="copyAllSocks"><Copy :size="15" />复制已启用连接</UiButton>
        <UiButton v-if="tab === 'third'" variant="primary" @click="openThird()"><Plus :size="15" />添加代理</UiButton>
      </template>
    </PageHeader>

    <div class="flex flex-wrap items-center gap-3">
      <SectionTabs v-model="tab" :items="tabs" />
    </div>

    <div v-if="!native && exitsView" class="rounded-xl border border-warn/25 bg-warn-soft px-4 py-3 text-xs leading-relaxed text-warn">
      当前为 {{ exitsView.panel_info || exitsView.backend }} 模式。VPS SOCKS5 与第三方代理只在内置 Xray 模式可用；原始 SSTP 检测仍可查看。
    </div>

    <div v-if="exitsLoading && !exitsView" class="rounded-xl border border-line bg-surface p-4"><SkeletonBlock :lines="5" /></div>

    <template v-else-if="tab === 'socks'">
      <Panel title="VPS SOCKS5" description="这些入口由 VPS 转换并持续提供服务；复制到其他设备时，VPS 和对应出口都需要在线。">
        <div v-if="!socksRows.length" class="py-4">
          <EmptyState compact title="还没有可用出口" description="先创建出口并等待就绪，再开启外部访问。">
            <template #icon><Plug :size="26" /></template>
          </EmptyState>
        </div>
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[760px] text-left text-xs">
            <caption class="sr-only">VPS SOCKS5 入口</caption>
            <thead>
              <tr class="border-b border-line text-[11px] text-faint">
                <th class="py-2 pr-3 font-medium">出口</th>
                <th class="py-2 pr-3 font-medium">实际 IP</th>
                <th class="py-2 pr-3 font-medium">内部端口</th>
                <th class="py-2 pr-3 font-medium">外部端口</th>
                <th class="py-2 pr-3 font-medium">状态</th>
                <th class="py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in socksRows" :key="row.exit.slot" class="border-b border-line last:border-b-0">
                <td class="py-2.5 pr-3">
                  <div class="font-medium">{{ exitName(row.exit) }}</div>
                  <div class="text-[10px] text-faint">{{ countryName(row.exit.region) }}</div>
                </td>
                <td class="py-2.5 pr-3 font-mono text-[11px]">{{ row.exit.exit_ip || "—" }}</td>
                <td class="tnum py-2.5 pr-3">{{ row.exit.port || "—" }}</td>
                <td class="tnum py-2.5 pr-3">{{ row.exit.public_socks?.port || "未开启" }}</td>
                <td class="py-2.5 pr-3"><StatusBadge :label="exitStatusLabel(row.exit.status)" :tone="exitStatusTone(row.exit.status)" size="sm" /></td>
                <td class="py-2.5 text-right">
                  <div class="flex justify-end gap-1">
                    <UiButton size="sm" variant="ghost" @click="openSocksDetail(row)">详情</UiButton>
                    <CopyButton v-if="row.link?.url" :text="row.link.url" label="复制" size="sm" variant="ghost" />
                    <UiButton
                      v-if="native && row.exit.status === 'up'"
                      size="sm"
                      variant="ghost"
                      :loading="socksMutation.isPending.value"
                      @click="socksMutation.mutate({ slot: row.exit.slot, enabled: !row.enabled })"
                    >
                      {{ row.enabled ? "关闭" : "开启" }}
                    </UiButton>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </Panel>
    </template>

    <template v-else-if="tab === 'sstp'">
      <div class="space-y-4">
        <Panel title="检测与更新" description="自动检测与手动任务共用本页并发额度；SSTP 不占用现有出口数量。">
          <form class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" @submit.prevent="sstpSettingsMutation.mutate()">
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">检测与建连并发</span>
              <input v-model.number="sstpSettings.parallelism" name="sstp-parallelism" type="number" min="1" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
            </label>
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">自动检测间隔（分钟，0 关闭）</span>
              <input v-model.number="sstpSettings.interval_minutes" name="sstp-interval" type="number" min="0" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
            </label>
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">结果有效期（分钟）</span>
              <input v-model.number="sstpSettings.valid_minutes" name="sstp-valid" type="number" min="1" max="10080" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
            </label>
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">单节点超时（秒）</span>
              <input v-model.number="sstpSettings.timeout_seconds" name="sstp-timeout" type="number" min="10" max="3600" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
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
            <input v-model="sstpSearch" name="sstp-search" type="search" placeholder="搜索节点或 IP" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
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

    <template v-else-if="tab === 'advanced'">
      <AdvancedAccessPanel :exits="exits" :backend="exitsView?.backend || ''" />
    </template>

    <template v-else-if="tab === 'third'">
      <div class="space-y-4">
        <div class="rounded-xl border border-line bg-surface-2 px-4 py-3 text-xs leading-relaxed text-muted">
          手动添加自己的 SOCKS5，独立加入订阅。流量经本 VPS 转发；供应商账号和密码不会进入客户端订阅。
        </div>

        <Panel flush>
          <div v-if="thirdLoading && !thirdData" class="p-4"><SkeletonBlock :lines="5" /></div>
          <div v-else-if="!thirdRows.length" class="p-4">
            <EmptyState title="还没有第三方代理" description="添加固定 SOCKS5 地址后，可以独立检测并选择是否加入订阅。">
              <template #icon><Plug :size="26" /></template>
              <template #action><UiButton size="sm" variant="primary" @click="openThird()"><Plus :size="14" />添加代理</UiButton></template>
            </EmptyState>
          </div>
          <div v-else class="overflow-x-auto">
            <table class="w-full min-w-[980px] text-left text-xs">
              <caption class="sr-only">第三方 SOCKS5 代理</caption>
              <thead>
                <tr class="border-b border-line text-[11px] text-faint">
                  <th class="px-4 py-2 font-medium">名称 / 服务器</th>
                  <th class="px-2 py-2 font-medium">手动标注</th>
                  <th class="px-2 py-2 font-medium">实际出口 / 地区</th>
                  <th class="px-2 py-2 font-medium">检测</th>
                  <th class="px-2 py-2 font-medium">订阅</th>
                  <th class="px-4 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in thirdRows" :key="row.id" class="border-b border-line last:border-b-0 hover:bg-surface-2">
                  <td class="px-4 py-2.5">
                    <div class="font-medium">{{ row.name || row.remark || `代理 ${row.id}` }}</div>
                    <div class="font-mono text-[10px] text-faint">{{ row.host }}:{{ row.port }} → {{ row.entry_port }}</div>
                  </td>
                  <td class="px-2 py-2.5">
                    <div>{{ row.country ? `${countryName(row.country)}（${row.country}）` : "未标注" }}</div>
                    <div class="text-[10px] text-faint">{{ row.city || "—" }}</div>
                  </td>
                  <td class="px-2 py-2.5">
                    <div class="font-mono text-[11px]">{{ row.check?.actual_ip || "—" }}</div>
                    <div class="text-[10px] text-faint">{{ row.check?.country ? countryName(row.check.country) : "地区未知" }}</div>
                  </td>
                  <td class="px-2 py-2.5">
                    <StatusBadge :label="checkStatusLabel(row.check?.status || 'not_tested')" :tone="checkStatusTone(row.check?.status || 'not_tested')" size="sm" />
                    <div v-if="row.check?.code" class="mt-1 text-[10px] text-bad">{{ errorLabel(row.check.code) }}</div>
                  </td>
                  <td class="px-2 py-2.5">
                    <div class="flex flex-col gap-1">
                      <label class="flex items-center gap-1.5 text-[10px] text-muted">
                        <input type="checkbox" name="third-enabled" :checked="row.enabled" class="accent-accent" @change="toggleThird(row, 'toggle')" />启用
                      </label>
                      <label class="flex items-center gap-1.5 text-[10px] text-muted">
                        <input type="checkbox" name="third-subscribe" :checked="row.subscribe" class="accent-accent" @change="toggleThird(row, 'subscribe')" />加入订阅
                      </label>
                    </div>
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <div class="flex justify-end gap-1">
                      <UiButton size="sm" variant="ghost" :loading="checkingThird.has(row.id)" @click="checkThird(row)">检测</UiButton>
                      <UiButton size="sm" variant="ghost" @click="openThird(row.id)">编辑</UiButton>
                      <UiButton size="sm" variant="ghost" @click="deleteRow = row; deleteOpen = true"><Trash2 :size="13" /></UiButton>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </template>

    <DialogShell :open="thirdOpen" :title="thirdEditing ? '编辑第三方 SOCKS5' : '添加第三方 SOCKS5'" description="供应商账号和密码只保存在本机；客户端订阅不会包含它们。" size="md" @update:open="thirdOpen = $event">
      <form class="space-y-4" @submit.prevent="saveThird">
        <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px]">
          <label class="block">
            <span class="mb-1 block text-xs font-medium">服务器域名或 IP</span>
            <input v-model="thirdForm.host" name="proxy-host" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" placeholder="proxy.example.com" />
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium">端口</span>
            <input v-model.number="thirdForm.port" name="proxy-port" type="number" min="1" max="65535" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block">
            <span class="mb-1 block text-xs font-medium">账号</span>
            <input v-model="thirdForm.username" name="proxy-username" required autocomplete="username" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium">密码{{ thirdEditing ? "（留空保留）" : "" }}</span>
            <input v-model="thirdForm.password" name="proxy-password" type="password" :required="!thirdEditing" autocomplete="new-password" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
        </div>
        <div class="grid gap-3 sm:grid-cols-3">
          <label class="block">
            <span class="mb-1 block text-xs font-medium">国家代码（可选）</span>
            <input v-model="thirdForm.country" name="proxy-country" maxlength="2" list="third-countries" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm uppercase outline-none focus:border-accent" />
            <datalist id="third-countries">
              <option v-for="country in thirdCountries" :key="country.code" :value="country.code">{{ country.name }}</option>
            </datalist>
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium">城市（可选）</span>
            <input v-model="thirdForm.city" name="proxy-city" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium">备注（可选）</span>
            <input v-model="thirdForm.remark" name="proxy-remark" maxlength="160" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
          </label>
        </div>
        <div class="flex flex-wrap gap-4 rounded-lg border border-line p-3">
          <label class="flex items-center gap-2 text-xs"><ToggleSwitch v-model="thirdForm.enabled" label="启用代理" />启用</label>
          <label class="flex items-center gap-2 text-xs"><ToggleSwitch v-model="thirdForm.subscribe" label="加入订阅" />加入订阅</label>
        </div>
        <p class="text-[11px] leading-relaxed text-faint">手填国家和城市不是检测证据；检测结果单独展示，地区未知时保留待验证。</p>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="thirdOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="thirdSaving">保存</UiButton>
        </div>
      </form>
    </DialogShell>

    <DialogShell
      :open="socksDetailOpen"
      :title="socksDetail ? `${exitName(socksDetail.exit)} · SOCKS5` : 'SOCKS5 详情'"
      description="查看并复制服务端 SOCKS5 入口的完整连接信息。"
      size="md"
      @update:open="socksDetailOpen = $event"
    >
      <div v-if="socksDetail" class="space-y-4">
        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
          <div><dt class="text-faint">实际出口 IP</dt><dd class="mt-0.5 font-mono">{{ socksDetail.exit.exit_ip || "—" }}</dd></div>
          <div><dt class="text-faint">服务器地址</dt><dd class="mt-0.5 font-mono">{{ exitsView?.public_ip || "本机地址" }}</dd></div>
          <div><dt class="text-faint">内部端口</dt><dd class="tnum mt-0.5">{{ socksDetail.exit.port || "—" }}</dd></div>
          <div><dt class="text-faint">外部端口</dt><dd class="tnum mt-0.5">{{ socksDetail.exit.public_socks?.port || "未开启" }}</dd></div>
        </dl>

        <section class="rounded-lg border border-line p-3">
          <div class="mb-2 flex items-center justify-between gap-2">
            <h3 class="text-xs font-semibold text-ink">SOCKS5 凭据</h3>
            <span class="text-[10px] text-faint">两项留空保存会随机重置</span>
          </div>
          <div class="grid gap-2 sm:grid-cols-2">
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">账号</span>
              <input v-model="socksUser" name="socks-user" autocomplete="off" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 font-mono text-xs outline-none focus:border-accent" />
            </label>
            <label class="block">
              <span class="mb-1 block text-[11px] text-faint">口令</span>
              <input v-model="socksPass" name="socks-pass" autocomplete="off" class="h-8 w-full rounded-md border border-line-strong bg-surface px-2 font-mono text-xs outline-none focus:border-accent" />
            </label>
          </div>
          <UiButton size="sm" class="mt-2" :loading="socksCredMutation.isPending.value" @click="socksCredMutation.mutate()">保存凭据</UiButton>
        </section>

        <div class="space-y-2">
          <div class="flex items-center justify-between gap-2">
            <span class="text-xs font-medium">外部连接地址</span>
            <CopyButton v-if="socksDetail.enabled" :text="socksAddress(socksDetail.exit, true)" label="复制地址" size="sm" />
          </div>
          <p class="break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-[11px] text-muted">
            {{ socksDetail.enabled ? socksAddress(socksDetail.exit, true) : "外部访问未开启；先在下方开启外部端口。" }}
          </p>
        </div>

        <div class="space-y-2">
          <div class="flex items-center justify-between gap-2">
            <span class="text-xs font-medium">服务器本机地址</span>
            <CopyButton :text="socksAddress(socksDetail.exit, false)" label="复制地址" size="sm" />
          </div>
          <p class="break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-[11px] text-muted">{{ socksAddress(socksDetail.exit, false) }}</p>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3">
          <p class="text-[11px] text-faint">外部端口需在防火墙放行；SOCKS5 连接本身不加密。</p>
          <UiButton
            v-if="native"
            size="sm"
            :loading="socksMutation.isPending.value"
            @click="socksMutation.mutate({ slot: socksDetail.exit.slot, enabled: !socksDetail.enabled })"
          >
            {{ socksDetail.enabled ? "关闭外部访问" : "开启外部访问" }}
          </UiButton>
        </div>
      </div>
    </DialogShell>

    <ConfirmDialog
      :open="deleteOpen"
      title="删除第三方代理"
      :description="`将删除 ${deleteRow?.name || deleteRow?.host || '该代理'} 及其接入入口，相关客户端连接会中断。`"
      confirm-label="删除"
      :busy="deleteMutation.isPending.value"
      @update:open="deleteOpen = $event"
      @confirm="deleteRow && deleteMutation.mutate(deleteRow.id)"
    />
  </div>
</template>
