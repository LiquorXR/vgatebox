<script setup lang="ts">
import { Copy, Plug } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, ref } from "vue";
import { post } from "@/api/client";
import { keys } from "@/api/queries";
import type { Exit, SocksLink } from "@/api/types";
import CopyButton from "@/components/CopyButton.vue";
import DialogShell from "@/components/DialogShell.vue";
import EmptyState from "@/components/EmptyState.vue";
import Panel from "@/components/Panel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { useAccessError } from "@/lib/access-error";
import { copyText } from "@/lib/clipboard";
import { countryName } from "@/lib/format";
import { exitName, exitStatusLabel, exitStatusTone } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  exits: Exit[];
  links: SocksLink[];
  publicIp: string;
}>();

const queryClient = useQueryClient();
const accessError = useAccessError();

const socksRows = computed(() => {
  const links = new Map(props.links.map((link) => [link.slot, link]));
  return props.exits.map((exit) => ({
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
  const host = external ? props.publicIp || window.location.hostname : "127.0.0.1";
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
    return post("api/cred", {
      slot: socksDetail.value.exit.slot,
      user: socksUser.value.trim(),
      pass: socksPass.value.trim(),
    });
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
  const urls = props.links.map((link) => link.url);
  if (!urls.length) {
    toast("没有已启用且就绪的 SOCKS5 连接", { tone: "error" });
    return;
  }
  await copySocks(urls.join("\n"));
}
</script>

<template>
  <Panel title="VPS SOCKS5" description="这些入口由 VPS 转换并持续提供服务；复制到其他设备时，VPS 和对应出口都需要在线。">
    <template #actions>
      <UiButton size="sm" @click="copyAllSocks"><Copy :size="14" />复制已启用连接</UiButton>
    </template>
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
                  v-if="row.exit.status === 'up'"
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
        <div><dt class="text-faint">服务器地址</dt><dd class="mt-0.5 font-mono">{{ publicIp || "本机地址" }}</dd></div>
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
        <p class="text-[11px] text-faint">外部端口需在防火墙放行 TCP/UDP；SOCKS5 连接本身不加密。</p>
        <UiButton
          size="sm"
          :loading="socksMutation.isPending.value"
          @click="socksMutation.mutate({ slot: socksDetail.exit.slot, enabled: !socksDetail.enabled })"
        >
          {{ socksDetail.enabled ? "关闭外部访问" : "开启外部访问" }}
        </UiButton>
      </div>
    </div>
  </DialogShell>
</template>
