<script setup lang="ts">
import { Download, Link2, Plus, RefreshCw, RotateCcw, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, ref } from "vue";
import { api, apiUrl, post, queryString } from "@/api/client";
import { keys, useInboundsQuery } from "@/api/queries";
import type { Exit, Inbound } from "@/api/types";
import ConfirmDialog from "./ConfirmDialog.vue";
import EmptyState from "./EmptyState.vue";
import Panel from "./Panel.vue";
import SkeletonBlock from "./SkeletonBlock.vue";
import StatusBadge from "./StatusBadge.vue";
import UiButton from "./UiButton.vue";
import InboundCreateDialog from "@/pages/access/InboundCreateDialog.vue";
import InboundDetailDrawer from "@/pages/access/InboundDetailDrawer.vue";
import { copyText } from "@/lib/clipboard";
import { truncate } from "@/lib/format";
import {
  bindLabel as bindLabelFor,
  bindValue,
  inboundKind,
  inboundName,
  inboundState,
  ownerOf as ownerOfInbound,
} from "@/lib/inbound";
import { exitName, exitStatusLabel } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  exits: Exit[];
}>();

const queryClient = useQueryClient();
const { data: inbounds, isLoading, isError, error, refetch } = useInboundsQuery(true);

const rows = computed<Inbound[]>(() => inbounds.value || []);
const ownerOf = (inbound: Inbound) => ownerOfInbound(inbound, props.exits);
const rowName = (inbound: Inbound) => inboundName(inbound, props.exits);
const rowKind = (inbound: Inbound) => inboundKind(inbound, props.exits);
const rowState = (inbound: Inbound) => inboundState(inbound, props.exits);
const bindLabel = (inbound: Inbound) => bindLabelFor(inbound, props.exits);

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

function openCreate() {
  createOpen.value = true;
}

// Detail drawer
const detailOpen = ref(false);
const detailInbound = ref<Inbound | null>(null);

function openDetail(inbound: Inbound) {
  detailInbound.value = inbound;
  detailOpen.value = true;
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
    detailOpen.value = false;
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
      detailOpen.value = false;
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
      detailOpen.value = false;
      toast("归档入口已清理", { tone: "success" });
      invalidate();
    },
    "清理",
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

    <InboundCreateDialog v-model:open="createOpen" :exits="exits" />

    <InboundDetailDrawer
      v-model:open="detailOpen"
      :inbound="detailInbound"
      :exits="exits"
      @delete-inbound="deleteInbound"
      @delete-managed="deleteManagedExit"
      @delete-archived="deleteArchivedInbound"
    />

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
