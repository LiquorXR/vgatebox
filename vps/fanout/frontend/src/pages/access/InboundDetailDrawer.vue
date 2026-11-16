<script setup lang="ts">
import { Plus, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { reactive, ref, watch } from "vue";
import { api, post, queryString } from "@/api/client";
import { keys } from "@/api/queries";
import type { Exit, Inbound, InboundDetail } from "@/api/types";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import CopyButton from "@/components/CopyButton.vue";
import DialogShell from "@/components/DialogShell.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import UiButton from "@/components/UiButton.vue";
import { bindLabel, inboundName, ownerOf } from "@/lib/inbound";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  open: boolean;
  inbound: Inbound | null;
  exits: Exit[];
}>();

const emit = defineEmits<{
  "update:open": [boolean];
  "delete-inbound": [Inbound];
  "delete-managed": [Inbound];
  "delete-archived": [Inbound];
}>();

const queryClient = useQueryClient();
const detail = ref<InboundDetail | null>(null);
const detailLoading = ref(false);
const detailForm = reactive({ nameMode: "auto", remark: "", port: 0, enable: true });

function setOpen(value: boolean) {
  emit("update:open", value);
}

function invalidate(): void {
  void queryClient.invalidateQueries({ queryKey: keys.inbounds });
  void queryClient.invalidateQueries({ queryKey: keys.exits });
  void queryClient.invalidateQueries({ queryKey: keys.console });
}

async function loadDetail(inbound: Inbound) {
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

watch(
  () => [props.open, props.inbound?.id],
  () => {
    if (props.open && props.inbound) void loadDetail(props.inbound);
  },
  { immediate: true },
);

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

const rotateEncryptionMutation = useMutation({
  mutationFn: () => {
    if (!detail.value) throw new Error("入口不存在");
    return post(`api/xui/encryption/rotate${queryString({ id: detail.value.id })}`, {});
  },
  onSuccess: async () => {
    toast("VLESS Encryption 参数已重新生成；旧链接会失效", { tone: "success" });
    await refreshDetail();
    invalidate();
  },
  onError: (error) => toastError(error, "重新生成加密参数失败"),
});

const setEncryptionMutation = useMutation({
  mutationFn: (enabled: boolean) => {
    if (!detail.value) throw new Error("入口不存在");
    return post(
      `api/xui/encryption/set${queryString({
        id: detail.value.id,
        enabled: enabled ? 1 : 0,
        key_type: "x25519",
      })}`,
      {},
    );
  },
  onSuccess: async (_, enabled) => {
    toast(enabled ? "VLESS Encryption 已启用" : "VLESS Encryption 已停用", { tone: "success" });
    await refreshDetail();
    invalidate();
  },
  onError: (error) => toastError(error, "更新 VLESS Encryption 失败"),
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

// Destructive confirmation for client-level actions handled inside the drawer.
const confirmOpen = ref(false);
const confirmBusy = ref(false);
const confirmTitle = ref("");
const confirmDescription = ref("");
const confirmLabel = ref("确认");
const confirmAction = ref<null | (() => Promise<void>)>(null);

function ask(title: string, description: string, action: () => Promise<void>, label = "确认") {
  confirmTitle.value = title;
  confirmDescription.value = description;
  confirmLabel.value = label;
  confirmAction.value = action;
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

function cloneInbound(inbound: InboundDetail) {
  ask(
    "复制到所有就绪出口",
    `以 ${inboundName(inbound, props.exits)} 为模板，为所有已就绪出口各创建一个接入入口。已有入口不会被删除。`,
    async () => {
      await cloneMutation.mutateAsync();
    },
    "复制",
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
</script>

<template>
  <DialogShell
    :open="open"
    :title="detail ? `${inboundName(detail, exits)} · ${detail.protocol.toUpperCase()}:${detail.port}` : '接入入口详情'"
    description="客户端凭据、绑定出口和入站参数。"
    variant="drawer"
    @update:open="setOpen"
  >
    <div v-if="detailLoading" class="py-4"><SkeletonBlock :lines="6" /></div>
    <div v-else-if="!detail" class="text-sm text-muted">入口不存在，可能已被删除。</div>
    <div v-else class="space-y-5">
      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-line bg-surface-2 p-3 text-xs">
        <div><dt class="text-faint">协议 / 传输</dt><dd class="mt-0.5">{{ detail.protocol }} · {{ detail.network || "tcp" }}</dd></div>
        <div><dt class="text-faint">安全</dt><dd class="mt-0.5">{{ detail.tls || "none" }}</dd></div>
        <div><dt class="text-faint">监听</dt><dd class="mt-0.5">{{ detail.listen || "0.0.0.0" }}</dd></div>
        <div><dt class="text-faint">绑定出口</dt><dd class="mt-0.5">{{ bindLabel(detail, exits) }}</dd></div>
        <div v-if="detail.vless_encryption"><dt class="text-faint">VLESS Encryption</dt><dd class="mt-0.5">已启用 · {{ detail.vless_encryption_type === "mlkem768" ? "ML-KEM-768" : "X25519" }}</dd></div>
      </dl>

      <form class="space-y-3" @submit.prevent="detailSaveMutation.mutate()">
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block"><span class="mb-1 block text-[11px] text-faint">名称方式</span><select v-model="detailForm.nameMode" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="auto">{{ detail?.host_direct ? "默认名称" : "跟随实际国家" }}</option><option value="custom">自定义名称</option></select></label>
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
        <UiButton v-if="detail.vless_encryption" size="sm" variant="secondary" :loading="rotateEncryptionMutation.isPending.value" @click="rotateEncryptionMutation.mutate()">重新生成加密参数</UiButton>
        <UiButton v-if="detail.protocol === 'vless' && !detail.vless_encryption" size="sm" variant="secondary" :loading="setEncryptionMutation.isPending.value" @click="setEncryptionMutation.mutate(true)">启用 VLESS Encryption</UiButton>
        <UiButton v-if="detail.vless_encryption" size="sm" variant="ghost" :loading="setEncryptionMutation.isPending.value" @click="setEncryptionMutation.mutate(false)">停用 VLESS Encryption</UiButton>
        <UiButton v-if="!detail.managed_slot" variant="danger" size="sm" @click="emit('delete-inbound', detail)"><Trash2 :size="13" />删除这个入口</UiButton>
      </div>
      <div v-if="detail.managed_slot && ownerOf(detail, exits)" class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3">
        <p class="text-[11px] text-faint">删除自动出口会把控制台目标数量减少 1；自动入口默认归档保留，可在确认框中同时清理。</p>
        <UiButton variant="danger" size="sm" @click="emit('delete-managed', detail)"><Trash2 :size="13" />删除出口</UiButton>
      </div>
      <div v-else-if="detail.managed_slot" class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3">
        <p class="text-[11px] text-faint">对应出口已不存在，这个自动入口处于归档阻断状态。</p>
        <UiButton variant="danger" size="sm" @click="emit('delete-archived', detail)"><Trash2 :size="13" />清理归档入口</UiButton>
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
  />
</template>
