<script setup lang="ts">
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, watch } from "vue";
import { post, queryString } from "@/api/client";
import { keys } from "@/api/queries";
import type { Exit } from "@/api/types";
import DialogShell from "@/components/DialogShell.vue";
import UiButton from "@/components/UiButton.vue";
import { exitName, exitStatusLabel } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  open: boolean;
  exits: Exit[];
}>();

const emit = defineEmits<{
  "update:open": [boolean];
  created: [];
}>();

const queryClient = useQueryClient();
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
  vless_encryption: false,
  vless_encryption_type: "x25519",
  kcp_mtu: "",
  kcp_tti: "",
  kcp_uplink: "",
  kcp_downlink: "",
});
const realityAllowed = computed(() => ["tcp", "xhttp", "grpc"].includes(createForm.network));
const visionAllowed = computed(
  () =>
    createForm.protocol === "vless" &&
    (createForm.vless_encryption ||
      (createForm.network === "tcp" && createForm.security !== "none")),
);
const needPath = computed(() => ["ws", "httpupgrade", "xhttp", "grpc"].includes(createForm.network));

function resetCreate() {
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
    vless_encryption: false,
    vless_encryption_type: "x25519",
    kcp_mtu: "",
    kcp_tti: "",
    kcp_uplink: "",
    kcp_downlink: "",
  });
}

watch(
  () => props.open,
  (open) => {
    if (open) resetCreate();
  },
  { immediate: true },
);

watch(
  () => [createForm.network, createForm.security, createForm.vless_encryption],
  () => {
    if (!realityAllowed.value && createForm.security === "reality") createForm.security = "none";
    if (!visionAllowed.value) createForm.vision = false;
  },
);

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
      kcp_mtu: createForm.kcp_mtu,
      kcp_tti: createForm.kcp_tti,
      kcp_uplink: createForm.kcp_uplink,
      kcp_downlink: createForm.kcp_downlink,
      vless_encryption_type: createForm.vless_encryption_type,
    };
    params.slot = createForm.slot;
    if (createForm.vision) params.vision = 1;
    if (createForm.vless_encryption) params.vless_encryption = 1;
    return post(`api/panel/inbound/new${queryString(params)}`, {});
  },
  onSuccess: (value) => {
    const result = value as { port?: number };
    emit("update:open", false);
    toast(`入口已创建${result.port ? `，端口 ${result.port}` : ""}`, { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.inbounds });
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.console });
    emit("created");
  },
  onError: (error) => toastError(error, "创建入口失败"),
});

const createSecurityOptions = computed(() => [
  { value: "none", label: "无" },
  { value: "tls", label: "TLS" },
  { value: "reality", label: "REALITY", disabled: !realityAllowed.value },
]);
</script>

<template>
  <DialogShell
    :open="open"
    title="新增接入入口"
    description="等待中的出口也可以预先绑定；未绑定时流量阻断。"
    size="lg"
    @update:open="emit('update:open', $event)"
  >
    <form class="space-y-4" @submit.prevent="createMutation.mutate()">
      <label class="block">
        <span class="mb-1 block text-xs font-medium">绑定出口</span>
        <select v-model.number="createForm.slot" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent">
          <option :value="0">未绑定（阻断流量）</option>
          <option v-for="exit in exits" :key="exit.slot" :value="exit.slot">{{ exitName(exit) }} · {{ exitStatusLabel(exit.status) }}</option>
        </select>
      </label>
      <div class="grid gap-3 sm:grid-cols-3">
        <label class="block"><span class="mb-1 block text-xs font-medium">协议</span><select v-model="createForm.protocol" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="vless">VLESS</option></select></label>
        <label class="block"><span class="mb-1 block text-xs font-medium">传输</span><select v-model="createForm.network" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option value="tcp">TCP / RAW</option><option value="ws">WebSocket</option><option value="grpc">gRPC</option><option value="httpupgrade">HTTPUpgrade</option><option value="xhttp">XHTTP</option><option value="mkcp">mKCP</option></select></label>
        <label class="block"><span class="mb-1 block text-xs font-medium">安全</span><select v-model="createForm.security" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-2 text-sm"><option v-for="option in createSecurityOptions" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select></label>
      </div>
      <p v-if="createForm.protocol === 'vless' && createForm.security === 'none' && !createForm.vless_encryption" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
        VLESS + 无传输安全层按官方要求只用于私网；公网请改用 TLS/REALITY，或启用 VLESS Encryption。
      </p>
      <p v-if="createForm.network === 'mkcp'" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
        v2rayN 可导入 VLESS + mKCP；Mihomo 无法表示 mKCP，Clash 订阅会跳过该入口。
      </p>
      <label v-if="createForm.protocol === 'vless'" class="flex flex-wrap items-center gap-3 rounded-lg border border-line p-3 text-xs text-muted">
        <span class="flex items-center gap-2"><input v-model="createForm.vless_encryption" type="checkbox" class="accent-accent" />启用 VLESS Encryption</span>
        <select v-model="createForm.vless_encryption_type" :disabled="!createForm.vless_encryption" class="h-8 rounded-lg border border-line-strong bg-surface px-2 text-xs disabled:opacity-50">
          <option value="x25519">X25519 (native)</option>
          <option value="mlkem768">ML-KEM-768</option>
        </select>
      </label>
      <label v-if="visionAllowed" class="flex items-center gap-2 text-xs text-muted"><input v-model="createForm.vision" type="checkbox" class="accent-accent" />启用 xtls-rprx-vision</label>
      <div v-if="createForm.security === 'tls'" class="grid gap-3 sm:grid-cols-3">
        <label class="block"><span class="mb-1 block text-[11px] text-faint">域名或 IP（SNI）</span><input v-model="createForm.sni" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空使用 localhost" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">证书路径</span><input v-model="createForm.cert" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空生成自签证书" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">私钥路径</span><input v-model="createForm.key" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
      </div>
      <label v-if="createForm.security === 'reality'" class="block"><span class="mb-1 block text-[11px] text-faint">借用站点（留空使用默认）</span><input v-model="createForm.dest" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="www.tesla.com:443" /></label>
      <div v-if="createForm.network === 'mkcp'" class="grid gap-3 sm:grid-cols-4">
        <label class="block"><span class="mb-1 block text-[11px] text-faint">MTU</span><input v-model="createForm.kcp_mtu" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="1350" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">TTI</span><input v-model="createForm.kcp_tti" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="50" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">上行 MB/s</span><input v-model="createForm.kcp_uplink" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="5" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">下行 MB/s</span><input v-model="createForm.kcp_downlink" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="20" /></label>
      </div>
      <label v-if="needPath" class="block"><span class="mb-1 block text-[11px] text-faint">{{ createForm.network === "grpc" ? "服务名" : "路径" }}</span><input v-model="createForm.path" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 font-mono text-sm" placeholder="留空自动生成" /></label>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block"><span class="mb-1 block text-[11px] text-faint">端口（留空随机分配）</span><input v-model="createForm.port" inputmode="numeric" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" /></label>
        <label class="block"><span class="mb-1 block text-[11px] text-faint">备注</span><input v-model="createForm.remark" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm" placeholder="留空自动命名" /></label>
      </div>
      <div class="flex justify-end gap-2 pt-1">
        <UiButton variant="ghost" @click="emit('update:open', false)">取消</UiButton>
        <UiButton type="submit" variant="primary" :loading="createMutation.isPending.value">创建</UiButton>
      </div>
    </form>
  </DialogShell>
</template>
