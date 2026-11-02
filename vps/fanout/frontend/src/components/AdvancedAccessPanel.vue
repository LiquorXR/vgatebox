<script setup lang="ts">
import { Download, Link2, Plus, RefreshCw, RotateCcw, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { api, apiUrl, post, queryString } from "@/api/client";
import { keys, useInboundsQuery } from "@/api/queries";
import type { Exit, Inbound, InboundDetail } from "@/api/types";
import ConfirmDialog from "./ConfirmDialog.vue";
import CopyButton from "./CopyButton.vue";
import DialogShell from "./DialogShell.vue";
import EmptyState from "./EmptyState.vue";
import Panel from "./Panel.vue";
import SkeletonBlock from "./SkeletonBlock.vue";
import StatusBadge from "./StatusBadge.vue";
import UiButton from "./UiButton.vue";
import { copyText } from "@/lib/clipboard";
import { truncate } from "@/lib/format";
import { exitName, exitStatusLabel, exitStatusTone } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  exits: Exit[];
}>();

const queryClient = useQueryClient();
const { data: inbounds, isLoading, isError, error, refetch } = useInboundsQuery(true);

const rows = computed<Inbound[]>(() => inbounds.value || []);

function ownerOf(inbound: Inbound): Exit | null {
  if (inbound.bound_slot) return props.exits.find((exit) => exit.slot === inbound.bound_slot) || null;
  if (inbound.bound_to) return props.exits.find((exit) => exit.host === inbound.bound_to) || null;
  return null;
}

function rowName(inbound: Inbound): string {
  const owner = ownerOf(inbound);
  if (inbound.managed_slot) {
    if (owner) return exitName(owner);
    return inbound.remark ? `${inbound.remark} · 归档` : "归档入口";
  }
  if (inbound.host_direct) return "VPS 本机直出";
  return inbound.remark || `${inbound.protocol.toUpperCase()} · ${inbound.port}`;
}

function rowKind(inbound: Inbound): string {
  if (inbound.host_direct) return "本机直出";
  if (inbound.managed_slot) return ownerOf(inbound) ? "自动入口" : "归档入口";
  if (inbound.manual) return "手动入口";
  return "接入入口";
}

function rowState(inbound: Inbound): { label: string; tone: "ok" | "warn" | "bad" | "info" | "neutral" } {
  if (inbound.managed_slot && !ownerOf(inbound)) return { label: "出口已删除 · 归档阻断", tone: "bad" };
  if (!inbound.enable) return { label: "已停用", tone: "neutral" };
  if (inbound.host_direct) return { label: "已启用", tone: "ok" };
  const owner = ownerOf(inbound);
  if (!owner) return { label: "未绑定 · 阻断", tone: "bad" };
  return { label: exitStatusLabel(owner.status), tone: exitStatusTone(owner.status) };
}

function bindValue(inbound: Inbound): string {
  return `slot:${inbound.bound_slot || 0}`;
}

function bindLabel(inbound: Inbound): string {
  const owner = ownerOf(inbound);
  if (inbound.host_direct) return "独立使用服务器网络";
  if (inbound.managed_slot) {
    if (owner) return `随出口 ${exitName(owner)}`;
    const slot = inbound.bound_slot ? `出口 ${String(inbound.bound_slot).padStart(2, "0")}` : "出口";
    return `${slot} 已删除 · 归档`;
  }
  return owner ? `${exitName(owner)} · ${exitStatusLabel(owner.status)}` : "未绑定（阻断）";
}

function invalidate(): void {
  void queryClient.invalidateQueries({ queryKey: keys.inbounds });
  void queryClient.invalidateQueries({ queryKey: keys.exits });
  void queryClient.invalidateQueries({ queryKey: keys.console });
}

const bindMutation = useMutation({
  mutationFn: ({ inbound, value }: { inbound: Inbound; value: string }) => {
    const slot = Number(value.replace("slot:", ""));
    return post(`api/xui/bind${queryString({ tag: inbound.tag, slot })}`, {});
  },
  onSuccess: () => {
    toast("绑定已更新", { tone: "success" });
    invalidate();
  },
  onError: (error) => toastError(error, "绑定失败"),
});

function bindTo(inbound: Inbound, value: string) {
  bindMutation.mutate({ inbound, value });
}

const toggleMutation = useMutation({
  mutationFn: ({ inbound, enabled }: { inbound: Inbound; enabled: boolean }) =>
    post(`api/panel/inbound/update${queryString({ id: inbound.id, enable: enabled ? 1 : 0 })}`, {}),
  onSuccess: () => {
    toast("入口状态已更新", { tone: "success" });
    invalidate();
  },
  onError: (error) => toastError(error, "更新入口失败"),
});

// Create dialog
const createOpen = ref(false);
const createForm = reactive({
  slot: 0,
  protocol: "vless",
  network: "xhttp",
  security: "reality",
  vision: false,
  port: "",
  remark: "",
  path: "",
  sni: "",
  cert: "",
  key: "",
  dest: "",
});
const realityAllowed = computed(() => ["tcp", "xhttp", "grpc"].includes(createForm.network));
const visionAllowed = computed(
  () => createForm.protocol === "vless" && createForm.network === "tcp" && createForm.security !== "none",
);
const needPath = computed(() => ["ws", "httpupgrade", "xhttp", "grpc"].includes(createForm.network));

watch(
  () => [createForm.network, createForm.protocol, createForm.security],
  () => {
    if (!realityAllowed.value && createForm.security === "reality") createForm.security = "none";
    if (!visionAllowed.value) createForm.vision = false;
  },
);

function openCreate() {
  Object.assign(createForm, {
    slot: props.exits.find((exit) => exit.status === "up")?.slot || 0,
    protocol: "vless",
    network: "xhttp",
    security: "reality",
    vision: false,
    port: "",
    remark: "",
    path: "",
    sni: "",
    cert: "",
    key: "",
    dest: "",
  });
  createOpen.value = true;
}

const createMutation = useMutation({
  mutationFn: () => {
    const params: Record<string, string | number> = {
      protocol: createForm.protocol,
      network: createForm.network,
      security: createForm.security,
      port: createForm.port.trim(),
      remark: createForm.remark.trim(),
      path: createForm.path.trim(),
      sni: createForm.sni.trim(),
      cert: createForm.cert.trim(),
      key: createForm.key.trim(),
      dest: createForm.dest.trim(),
    };
    params.slot = createForm.slot;
    if (createForm.vision) params.vision = 1;
    return post(`api/panel/inbound/new${queryString(params)}`, {});
  },
  onSuccess: (value) => {
    const result = value as { port?: number };
    createOpen.value = false;
    toast(`入口已创建${result.port ? `，端口 ${result.port}` : ""}`, { tone: "success" });
    invalidate();
  },
  onError: (error) => toastError(error, "创建入口失败"),
});

// Detail drawer
const detailOpen = ref(false);
const detailLoading = ref(false);
const detail = ref<InboundDetail | null>(null);
const detailForm = reactive({ nameMode: "auto", remark: "", port: 0, enable: true });

async function openDetail(inbound: Inbound) {
  detailOpen.value = true;
  detailLoading.value = true;
  detail.value = null;
  try {
    const value = await api<InboundDetail>(`api/xui/detail${queryString({ id: inbound.id })}`);
    detail.value = value;
    Object.assign(detailForm, {
      nameMode: value.name_mode || "auto",
      remark: value.remark || "",
      port: value.port,
      enable: value.enable,
    });
  } catch (error) {
    toastError(error, "读取入口详情失败");
  } finally {
    detailLoading.value = false;
  }
}

async function refreshDetail() {
  if (!detail.value) return;
  const id = detail.value.id;
  try {
    const value = await api<InboundDetail>(`api/xui/detail${queryString({ id })}`);
    detail.value = value;
    Object.assign(detailForm, {
      nameMode: value.name_mode || "auto",
      remark: value.remark || "",
      port: value.port,
      enable: value.enable,
    });
    invalidate();
  } catch (error) {
    toastError(error, "刷新入口详情失败");
  }
}

const detailSaveMutation = useMutation({
  mutationFn: () => {
    if (!detail.value) throw new Error("入口不存在");
    const port = Number(detailForm.port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("端口须为 1–65535");
    return post(
      `api/panel/inbound/update${queryString({
        id: detail.value.id,
        port,
        remark: detailForm.remark.trim(),
        name_mode: detailForm.nameMode,
        enable: detailForm.enable ? 1 : 0,
      })}`,
      {},
    );
  },
  onSuccess: async () => {
    toast("入口已保存", { tone: "success" });
    await refreshDetail();
  },
  onError: (error) => toastError(error, "保存入口失败"),
});

const clientAddMutation = useMutation({
  mutationFn: () => {
    if (!detail.value) throw new Error("入口不存在");
    return post(`api/panel/client/add${queryString({ id: detail.value.id })}`, {});
  },
  onSuccess: async () => {
    toast("客户端已添加", { tone: "success" });
    await refreshDetail();
  },
  onError: (error) => toastError(error, "添加客户端失败"),
});

const cloneMutation = useMutation({
  mutationFn: () => {
    if (!detail.value) throw new Error("入口不存在");
    return post(`api/xui/clone${queryString({ id: detail.value.id })}`, {});
  },
  onSuccess: (value) => {
    const created = (value as { created?: number[] }).created?.length || 0;
    toast(`已创建 ${created} 个接入入口`, { tone: "success" });
    void refreshDetail();
    invalidate();
  },
  onError: (error) => toastError(error, "复制入口失败"),
});

function cloneInbound(inbound: InboundDetail) {
  ask(
    "复制到所有就绪出口",
    `以 ${rowName(inbound)} 为模板，为所有已就绪出口各创建一个接入入口。已有入口不会被删除。`,
    async () => {
      await cloneMutation.mutateAsync();
    },
    "复制",
  );
}

// Destructive confirmation
const confirmOpen = ref(false);
const confirmBusy = ref(false);
const confirmTitle = ref("");
const confirmDescription = ref("");
const confirmLabel = ref("确认");
const confirmAction = ref<null | (() => Promise<void>)>(null);
const confirmCleanupVisible = ref(false);
const confirmCleanup = ref(false);

function ask(
  title: string,
  description: string,
  action: () => Promise<void>,
  label = "确认",
  options: { cleanup?: boolean } = {},
) {
  confirmTitle.value = title;
  confirmDescription.value = description;
  confirmLabel.value = label;
  confirmAction.value = action;
  confirmCleanupVisible.value = !!options.cleanup;
  confirmCleanup.value = false;
  confirmOpen.value = true;
}

async function runConfirm() {
  if (!confirmAction.value) return;
  confirmBusy.value = true;
  try {
    await confirmAction.value();
    confirmOpen.value = false;
  } catch (error) {
    toastError(error, "操作失败");
  } finally {
    confirmBusy.value = false;
  }
}

function deleteInbound(inbound: Inbound) {
  ask("删除接入入口", `删除 ${rowName(inbound)} · ${inbound.protocol.toUpperCase()}:${inbound.port}？使用它的客户端链接会立即失效。`, async () => {
    await post(`api/xui/delete${queryString({ ids: inbound.id })}`, {});
    if (detail.value?.id === inbound.id) detailOpen.value = false;
    toast("入口已删除", { tone: "success" });
    invalidate();
  }, "删除");
}

function deleteManagedExit(inbound: Inbound) {
  const owner = ownerOf(inbound);
  if (!owner) {
    deleteArchivedInbound(inbound);
    return;
  }
  ask(
    "删除自动出口",
    `删除 ${exitName(owner)}？目标数量会自动减少 1，其他出口编号和端口保持不变。自动入口默认归档保留。`,
    async () => {
      const result = await post<{ ok: boolean; cleanup_error?: string }>("api/console/slot/delete", {
        slot: owner.slot,
        cleanup_archive: confirmCleanup.value,
      });
      if (detail.value?.id === inbound.id) detailOpen.value = false;
      if (result.cleanup_error) {
        toast(`出口已删除，但归档入口清理失败：${result.cleanup_error}`, { tone: "error" });
      } else {
        toast(confirmCleanup.value ? "自动出口和归档入口已删除" : "自动出口已删除，归档入口已保留", {
          tone: "success",
        });
      }
      invalidate();
    },
    "删除出口",
    { cleanup: true },
  );
}

function deleteArchivedInbound(inbound: Inbound) {
  ask(
    "清理归档入口",
    `删除 ${rowName(inbound)}？对应出口已不存在，本操作只清理这个归档入口及其客户端凭据。`,
    async () => {
      await post(`api/xui/delete${queryString({ ids: inbound.id })}`, {});
      if (detail.value?.id === inbound.id) detailOpen.value = false;
      toast("归档入口已清理", { tone: "success" });
      invalidate();
    },
    "清理",
  );
}

function resetClient(email: string) {
  if (!detail.value) return;
  ask(
    "重置客户端凭据",
    `重置 ${email} 的凭据？已经分发的旧链接会立即失效。`,
    async () => {
      await post(`api/panel/client/reset${queryString({ id: detail.value?.id, email })}`, {});
      toast("客户端凭据已重置", { tone: "success" });
      await refreshDetail();
    },
    "重置",
  );
}

function deleteClient(email: string) {
  if (!detail.value) return;
  ask(
    "删除客户端",
    `删除客户端 ${email}？它的链接会立即失效。`,
    async () => {
      await post(`api/panel/client/del${queryString({ id: detail.value?.id, email })}`, {});
      toast("客户端已删除", { tone: "success" });
      await refreshDetail();
    },
    "删除",
  );
}

function rollbackConfig() {
  ask(
    "恢复上次接入配置",
    "恢复上一次成功提交的内置入站、客户端凭据和绑定。所有 Xray 连接会短暂中断，出口策略不会改变。",
    async () => {
      await post("api/config/rollback", {});
      toast("已回滚接入配置", { tone: "success" });
      invalidate();
    },
    "恢复",
  );
}

// Export
const exporting = ref(false);
async function copyAllLinks() {
  const ids = rows.value.map((inbound) => inbound.id);
  if (!ids.length) {
    toast("还没有可导出的入口", { tone: "error" });
    return;
  }
  exporting.value = true;
  try {
    const value = await api<{ links: string[] }>(`api/xui/links${queryString({ ids: ids.join(",") })}`);
    await copyText((value.links || []).join("\n"));
    toast(`已复制 ${value.links?.length || 0} 条节点链接`, { tone: "success" });
  } catch (error) {
    toastError(error, "导出节点链接失败");
  } finally {
    exporting.value = false;
  }
}

function exportXray() {
  window.location.href = apiUrl("api/export/xray");
}

const createSecurityOptions = computed(() =>
  [
    { value: "none", label: "无" },
    { value: "tls", label: "TLS" },
    { value: "reality", label: "REALITY", disabled: !realityAllowed.value },
  ],
);
</script>

<template>
  <div class="space-y-4">
    <div class="rounded-xl border border-line bg-surface-2 px-4 py-3 text-xs leading-relaxed text-muted">
      这里管理客户端如何接入：入站协议、端口、绑定出口和客户端凭据。自动入口由出口数量维护；手动入口可以单独创建、停用或删除。修改接入配置可能短暂中断客户端连接。
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <UiButton @click="openCreate"><Plus :size="14" />新增接入入口</UiButton>
      <UiButton :loading="exporting" :disabled="!rows.length" @click="copyAllLinks"><Link2 :size="14" />复制全部节点链接</UiButton>
      <UiButton @click="exportXray"><Download :size="14" />导出 Xray 配置</UiButton>
      <UiButton variant="ghost" @click="rollbackConfig"><RotateCcw :size="14" />恢复上次配置</UiButton>
      <UiButton variant="ghost" :loading="isLoading" @click="refetch()"><RefreshCw :size="14" />刷新</UiButton>
    </div>

    <div v-if="isError" class="rounded-xl border border-bad/25 bg-bad-soft px-4 py-3 text-sm text-bad" role="alert">
      无法读取接入入口：{{ error?.message || "请稍后重试" }}
      <button type="button" class="ml-2 font-semibold underline" @click="refetch()">重试</button>
    </div>

    <Panel flush>
      <div v-if="isLoading && !inbounds" class="p-4"><SkeletonBlock :lines="6" /></div>
      <div v-else-if="!rows.length" class="p-4">
        <EmptyState title="还没有接入入口" description="先在出口页创建出口，或手动新增一个接入入口。">
          <template #icon><Link2 :size="26" /></template>
          <template #action><UiButton size="sm" variant="primary" @click="openCreate"><Plus :size="14" />新增入口</UiButton></template>
        </EmptyState>
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full min-w-[900px] text-left text-xs">
          <caption class="sr-only">接入入口列表</caption>
          <thead>
            <tr class="border-b border-line text-[11px] text-faint">
              <th class="px-4 py-2 font-medium">接入入口</th>
              <th class="px-3 py-2 font-medium">协议 / 端口</th>
              <th class="px-3 py-2 font-medium">绑定出口</th>
              <th class="px-3 py-2 font-medium">状态</th>
              <th class="px-4 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="inbound in rows" :key="inbound.id" class="border-b border-line last:border-b-0 hover:bg-surface-2">
              <td class="px-4 py-2.5">
                <div class="font-medium">{{ truncate(rowName(inbound), 32) }}</div>
                <div class="text-[10px] text-faint">{{ rowKind(inbound) }} · {{ inbound.tag }}</div>
              </td>
              <td class="px-3 py-2.5">
                <span class="font-mono text-[11px]">{{ inbound.protocol.toUpperCase() }}</span>
                <span class="text-faint"> · {{ inbound.port }}</span>
              </td>
              <td class="px-3 py-2.5">
                <select
                  v-if="!inbound.managed_slot && !inbound.host_direct"
                  :value="bindValue(inbound)"
                  class="h-8 max-w-[240px] rounded-md border border-line-strong bg-surface px-2 text-[11px] outline-none focus:border-accent"
                  :disabled="bindMutation.isPending.value"
                  @change="bindTo(inbound, ($event.target as HTMLSelectElement).value)"
                >
                  <option value="slot:0">未绑定（阻断）</option>
                  <option v-for="exit in exits" :key="exit.slot" :value="`slot:${exit.slot}`">
                    {{ exitName(exit) }} · {{ exitStatusLabel(exit.status) }}
                  </option>
                </select>
                <span v-else class="text-[11px] text-muted">{{ bindLabel(inbound) }}</span>
              </td>
              <td class="px-3 py-2.5"><StatusBadge :label="rowState(inbound).label" :tone="rowState(inbound).tone" size="sm" /></td>
              <td class="px-4 py-2.5 text-right">
                <div class="flex justify-end gap-1">
                  <UiButton size="sm" variant="ghost" @click="openDetail(inbound)">详情</UiButton>
                  <UiButton
                    v-if="!inbound.host_direct"
                    size="sm"
                    variant="ghost"
                    :loading="toggleMutation.isPending.value"
                    @click="toggleMutation.mutate({ inbound, enabled: !inbound.enable })"
                  >
                    {{ inbound.enable ? "停用" : "启用" }}
                  </UiButton>
                  <UiButton
                    v-if="inbound.managed_slot && ownerOf(inbound)"
                    size="sm"
                    variant="ghost"
                    @click="deleteManagedExit(inbound)"
                  >
                    <Trash2 :size="13" />删除出口
                  </UiButton>
                  <UiButton
                    v-else-if="inbound.managed_slot"
                    size="sm"
                    variant="ghost"
                    @click="deleteArchivedInbound(inbound)"
                  >
                    <Trash2 :size="13" />清理入口
                  </UiButton>
                  <UiButton v-else size="sm" variant="ghost" @click="deleteInbound(inbound)">
                    <Trash2 :size="13" />删除
                  </UiButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Panel>

    <DialogShell :open="createOpen" title="新增接入入口" description="等待中的出口也可以预先绑定；未绑定时流量阻断。" size="lg" @update:open="createOpen = $event">
      <form class="space-y-4" @submit.prevent="createMutation.mutate()">
        <label class="block">
          <span class="mb-1 block text-xs font-medium">绑定出口</span>
          <select v-model.number="createForm.slot" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent">
            <option :value="0">未绑定（阻断流量）</option>
            <option v-for="exit in exits" :key="exit.slot" :value="exit.slot">{{ exitName(exit) }} · {{ exitStatusLabel(exit.status) }}</option>
          </select>
        </label>
        <div class="grid gap-3 sm:grid-cols-3">
          <label class="block"><span class="mb-1 block text-xs font-medium">协议</span><select v-model="createForm.protocol" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="vless">VLESS</option><option value="vmess">VMess</option><option value="trojan">Trojan</option></select></label>
          <label class="block"><span class="mb-1 block text-xs font-medium">传输</span><select v-model="createForm.network" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="tcp">TCP</option><option value="ws">WebSocket</option><option value="grpc">gRPC</option><option value="httpupgrade">HTTPUpgrade</option><option value="xhttp">XHTTP</option></select></label>
          <label class="block"><span class="mb-1 block text-xs font-medium">安全</span><select v-model="createForm.security" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option v-for="option in createSecurityOptions" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select></label>
        </div>
        <p v-if="createForm.network === 'xhttp' && createForm.protocol !== 'vless'" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
          Mihomo 不支持 {{ createForm.protocol.toUpperCase() }} + XHTTP；该入口会进入 v2rayN/Xray 订阅，但 Clash 订阅会跳过它。
        </p>
        <p v-if="createForm.protocol === 'trojan' && createForm.security === 'none'" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
          Mihomo 对 Trojan 强制启用 TLS；该入口会进入 v2rayN/Xray 订阅，但 Clash 订阅会跳过它。
        </p>
        <label v-if="visionAllowed" class="flex items-center gap-2 text-xs text-muted"><input v-model="createForm.vision" type="checkbox" class="accent-accent" />启用 xtls-rprx-vision</label>
        <div v-if="createForm.security === 'tls'" class="grid gap-3 sm:grid-cols-3">
          <label class="block"><span class="mb-1 block text-[11px] text-faint">域名 SNI</span><input v-model="createForm.sni" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空使用 localhost" /></label>
          <label class="block"><span class="mb-1 block text-[11px] text-faint">证书路径</span><input v-model="createForm.cert" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空生成自签证书" /></label>
          <label class="block"><span class="mb-1 block text-[11px] text-faint">私钥路径</span><input v-model="createForm.key" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
        </div>
        <label v-if="createForm.security === 'reality'" class="block"><span class="mb-1 block text-[11px] text-faint">借用站点（留空使用默认）</span><input v-model="createForm.dest" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="www.tesla.com:443" /></label>
        <label v-if="needPath" class="block"><span class="mb-1 block text-[11px] text-faint">{{ createForm.network === "grpc" ? "服务名" : "路径" }}</span><input v-model="createForm.path" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 font-mono text-sm" placeholder="留空自动生成" /></label>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block"><span class="mb-1 block text-[11px] text-faint">端口（留空随机分配）</span><input v-model="createForm.port" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
          <label class="block"><span class="mb-1 block text-[11px] text-faint">备注</span><input v-model="createForm.remark" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空自动命名" /></label>
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <UiButton variant="ghost" @click="createOpen = false">取消</UiButton>
          <UiButton type="submit" variant="primary" :loading="createMutation.isPending.value">创建</UiButton>
        </div>
      </form>
    </DialogShell>

    <DialogShell :open="detailOpen" :title="detail ? `${rowName(detail)} · ${detail.protocol.toUpperCase()}:${detail.port}` : '接入入口详情'" description="客户端凭据、绑定出口和入站参数。" variant="drawer" @update:open="detailOpen = $event">
      <div v-if="detailLoading" class="py-4"><SkeletonBlock :lines="6" /></div>
      <div v-else-if="!detail" class="text-sm text-muted">入口不存在，可能已被删除。</div>
      <div v-else class="space-y-5">
        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
          <div><dt class="text-faint">协议 / 传输</dt><dd class="mt-0.5">{{ detail.protocol }} · {{ detail.network || "tcp" }}</dd></div>
          <div><dt class="text-faint">安全</dt><dd class="mt-0.5">{{ detail.tls || "none" }}</dd></div>
          <div><dt class="text-faint">监听</dt><dd class="mt-0.5">{{ detail.listen || "0.0.0.0" }}</dd></div>
          <div><dt class="text-faint">绑定出口</dt><dd class="mt-0.5">{{ bindLabel(detail) }}</dd></div>
        </dl>

        <form class="space-y-3" @submit.prevent="detailSaveMutation.mutate()">
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="block"><span class="mb-1 block text-[11px] text-faint">名称方式</span><select v-model="detailForm.nameMode" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="auto">跟随实际国家</option><option value="custom">自定义名称</option></select></label>
            <label class="block"><span class="mb-1 block text-[11px] text-faint">端口</span><input v-model.number="detailForm.port" type="number" min="1" max="65535" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
          </div>
          <label v-if="detailForm.nameMode === 'custom'" class="block"><span class="mb-1 block text-[11px] text-faint">自定义名称</span><input v-model="detailForm.remark" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
          <label class="flex items-center gap-2 text-xs text-muted"><input v-model="detailForm.enable" type="checkbox" class="accent-accent" />启用这个入口</label>
          <UiButton type="submit" size="sm" variant="primary" :loading="detailSaveMutation.isPending.value">保存入口参数</UiButton>
        </form>

        <section>
          <div class="mb-2 flex items-center justify-between gap-2">
            <h3 class="text-xs font-semibold text-ink">客户端 · {{ detail.clients?.length || 0 }}</h3>
            <UiButton size="sm" :loading="clientAddMutation.isPending.value" @click="clientAddMutation.mutate()"><Plus :size="13" />添加客户端</UiButton>
          </div>
          <div v-if="!detail.clients?.length" class="rounded-lg border border-dashed border-line-strong px-3 py-4 text-center text-[11px] text-faint">没有客户端</div>
          <div v-else class="space-y-2">
            <div v-for="(client, index) in detail.clients" :key="client.email" class="rounded-lg border border-line p-3">
              <div class="flex flex-wrap items-center gap-2">
                <span class="min-w-0 flex-1 truncate text-xs font-medium">{{ client.email }}</span>
                <span v-if="!client.enable" class="text-[10px] text-faint">已停用</span>
                <CopyButton v-if="detail.links?.[index]" :text="detail.links[index]" size="sm" variant="ghost" label="复制" />
                <UiButton size="sm" variant="ghost" @click="resetClient(client.email)">重置</UiButton>
                <UiButton size="sm" variant="ghost" @click="deleteClient(client.email)"><Trash2 :size="13" /></UiButton>
              </div>
              <p v-if="detail.links?.[index]" class="mt-2 break-all rounded bg-surface-2 px-2 py-1 font-mono text-[10px] text-faint">{{ detail.links[index] }}</p>
            </div>
          </div>
        </section>

        <div class="flex flex-wrap items-center gap-2 border-t border-line pt-3">
          <UiButton size="sm" :loading="cloneMutation.isPending.value" @click="cloneInbound(detail)">复制到所有就绪出口</UiButton>
          <UiButton v-if="!detail.managed_slot" variant="danger" size="sm" @click="deleteInbound(detail)"><Trash2 :size="13" />删除这个入口</UiButton>
        </div>
        <div
          v-if="detail.managed_slot && ownerOf(detail)"
          class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3"
        >
          <p class="text-[11px] text-faint">删除自动出口会把控制台目标数量减少 1；自动入口默认归档保留，可在确认框中同时清理。</p>
          <UiButton variant="danger" size="sm" @click="deleteManagedExit(detail)"><Trash2 :size="13" />删除出口</UiButton>
        </div>
        <div
          v-else-if="detail.managed_slot"
          class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3"
        >
          <p class="text-[11px] text-faint">对应出口已不存在，这个自动入口处于归档阻断状态。</p>
          <UiButton variant="danger" size="sm" @click="deleteArchivedInbound(detail)"><Trash2 :size="13" />清理归档入口</UiButton>
        </div>
      </div>
    </DialogShell>

    <ConfirmDialog
      :open="confirmOpen"
      :title="confirmTitle"
      :description="confirmDescription"
      :confirm-label="confirmLabel"
      :busy="confirmBusy"
      @update:open="confirmOpen = $event"
      @confirm="runConfirm"
    >
      <label
        v-if="confirmCleanupVisible"
        class="flex items-start gap-2 rounded-lg border border-line p-3 text-xs text-muted"
      >
        <input v-model="confirmCleanup" type="checkbox" name="cleanup-archive" class="mt-0.5 accent-accent" />
        <span>
          同时清理归档入口
          <span class="mt-0.5 block text-[11px] text-faint">勾选后会删除该出口的自动入口和客户端凭据，无法恢复；不勾选则保留归档，之后重建同一编号时复用。</span>
        </span>
      </label>
    </ConfirmDialog>
  </div>
</template>
