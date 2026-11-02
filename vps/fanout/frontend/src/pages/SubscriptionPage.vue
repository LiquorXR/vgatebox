<script setup lang="ts">
import { Copy, ExternalLink, QrCode, RotateCcw, ShieldCheck } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, ref, watch } from "vue";
import { appUrl, post } from "@/api/client";
import { keys, useExitsQuery, useSubQuery, useThirdPartyQuery } from "@/api/queries";
import type { ThirdPartyRow } from "@/api/types";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import CopyButton from "@/components/CopyButton.vue";
import EmptyState from "@/components/EmptyState.vue";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import UiButton from "@/components/UiButton.vue";
import { copyText } from "@/lib/clipboard";
import { buildSubUrl, countSubEntries, subQrUrl, type SubFormat } from "@/lib/subscription";
import { toast, toastError } from "@/lib/toast";
import { countryName } from "@/lib/format";
import { exitName, exitStatusLabel, exitStatusTone } from "@/lib/status";

const queryClient = useQueryClient();
const { data: sub, isLoading: subLoading, isError: subError, error: subErrorValue, refetch: refetchSub } = useSubQuery(true);
const { data: view, isLoading: exitsLoading } = useExitsQuery(true);
const { data: thirdData } = useThirdPartyQuery(true);

const format = ref<SubFormat>("base64");
const vpnAll = ref(true);
const slots = ref<Set<number>>(new Set());
const direct = ref(true);
const thirdAll = ref(true);
const proxies = ref<Set<number>>(new Set());

const exits = computed(() => view.value?.exits || []);
const hostDirect = computed(() => view.value?.host_direct || []);
const directAvailable = computed(() => hostDirect.value.some((inbound) => inbound.enable));
const thirdProxies = computed<ThirdPartyRow[]>(() => thirdData.value?.proxies || []);

watch(directAvailable, (available) => {
  if (!available) direct.value = false;
});

const scope = computed(() => ({
  vpnAll: vpnAll.value,
  slots: slots.value,
  direct: direct.value && directAvailable.value,
  thirdAll: thirdAll.value,
  proxies: proxies.value,
}));

const url = computed(() => buildSubUrl(sub.value, format.value, scope.value));
const qr = computed(() => subQrUrl(url.value));
const counts = computed(() => countSubEntries(exits.value, hostDirect.value, thirdProxies.value, scope.value));

function toggleVpnAll(checked: boolean) {
  vpnAll.value = checked;
  if (!checked && slots.value.size === 0) {
    slots.value = new Set(exits.value.map((exit) => exit.slot));
  }
}

function toggleVpnSlot(slot: number, checked: boolean) {
  const next = new Set(slots.value);
  if (checked) next.add(slot);
  else next.delete(slot);
  slots.value = next;
}

function toggleThirdAll(checked: boolean) {
  thirdAll.value = checked;
  if (!checked && proxies.value.size === 0) {
    proxies.value = new Set(thirdProxies.value.map((proxy) => proxy.id));
  }
}

function toggleThird(id: number, checked: boolean) {
  const next = new Set(proxies.value);
  if (checked) next.add(id);
  else next.delete(id);
  proxies.value = next;
}

const resetOpen = ref(false);
const resetMutation = useMutation({
  mutationFn: () => post("api/sub/reset", {}),
  onSuccess: () => {
    resetOpen.value = false;
    toast("订阅口令已重置，旧地址立即失效", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.sub });
  },
  onError: (error) => toastError(error, "重置订阅口令失败"),
});

async function copyUrl() {
  try {
    await copyText(url.value);
    toast("订阅链接已复制", { tone: "success" });
  } catch (error) {
    toastError(error, "复制失败");
  }
}

async function copyNodes() {
  if (!url.value) return;
  try {
    const target = new URL(url.value);
    target.searchParams.set("target", "links");
    const response = await fetch(target.href, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) throw new Error(`读取节点链接失败（${response.status}）`);
    await copyText((await response.text()).trim());
    toast("节点链接已复制", { tone: "success" });
  } catch (error) {
    toastError(error, "复制节点链接失败");
  }
}

const readyCount = computed(() => exits.value.filter((exit) => exit.status === "up").length);
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="订阅" description="选择客户端格式和订阅范围，复制链接后在客户端更新。出口变化不需要重新导入。">
      <template #actions>
        <UiButton @click="resetOpen = true">
          <RotateCcw :size="15" />重置口令
        </UiButton>
        <UiButton variant="primary" :disabled="!url" @click="copyUrl">
          <Copy :size="15" />复制订阅
        </UiButton>
      </template>
    </PageHeader>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <div class="min-w-0 space-y-4">
        <Panel title="订阅链接" description="本机直出与自定义代理按启用和订阅设置输出；VPN Gate 仅输出已就绪入口。">
          <div v-if="subLoading || exitsLoading" class="space-y-3"><SkeletonBlock :lines="3" /></div>
          <div v-else-if="subError" class="text-sm text-bad">
            无法读取订阅：{{ subErrorValue?.message || "请稍后重试" }}
            <button type="button" class="ml-1 font-semibold underline" @click="refetchSub()">重试</button>
          </div>
          <div v-else-if="!sub" class="text-sm text-bad">订阅尚未就绪，请稍后刷新。</div>
          <div v-else class="space-y-4">
            <textarea
              :value="url"
              name="subscription-url"
              rows="3"
              readonly
              class="scrollbar-thin w-full resize-none rounded-lg border border-line-strong bg-surface-2 p-3 font-mono text-[11px] leading-relaxed text-muted outline-none focus:border-accent"
              aria-label="订阅链接"
            />
            <div class="flex flex-wrap items-end gap-3">
              <label class="block">
                <span class="mb-1 block text-[11px] text-faint">客户端格式</span>
                <select v-model="format" name="sub-format" class="h-9 rounded-lg border border-line-strong bg-surface px-3 text-xs outline-none focus:border-accent">
                  <option value="base64">v2rayN / 通用节点列表</option>
                  <option value="clash">Clash Verge / Rev（Mihomo）</option>
                </select>
              </label>
              <CopyButton :text="url" label="复制订阅链接" variant="primary" :disabled="!url" />
              <UiButton :disabled="!url" @click="copyNodes"><Copy :size="14" />复制节点链接</UiButton>
            </div>
            <p v-if="!url" class="text-xs text-warn">当前没有选中任何订阅内容，请至少开启一个来源。</p>
            <div class="rounded-lg border border-line bg-surface-2 px-3 py-2 text-[11px] leading-relaxed text-muted">
              当前范围：{{ counts.direct ? "VPS 本机直出 · " : "" }}{{ counts.custom }} 个自定义入口 · {{ counts.vpnInbounds }} 个 VPN Gate 入站（{{ counts.vpnExits }} 个就绪出口）。
            </div>
          </div>
        </Panel>

        <Panel title="选择订阅范围" description="全选包含今后新增的出口；取消全选后可逐项选择。">
          <div class="space-y-4">
            <div class="rounded-lg border border-line p-3">
              <label class="flex items-start justify-between gap-3">
                <span>
                  <span class="block text-sm font-medium">VPS 本机直出</span>
                  <span class="mt-0.5 block text-[11px] text-muted">
                    {{ directAvailable ? "使用服务器公网 IP，独立加入订阅。" : "尚未启用；可在总览或出口页开启。" }}
                  </span>
                </span>
                <input v-model="direct" name="sub-direct" type="checkbox" :disabled="!directAvailable" class="mt-1 accent-accent" />
              </label>
            </div>

            <div class="rounded-lg border border-line p-3">
              <label class="flex items-start justify-between gap-3">
                <span>
                  <span class="block text-sm font-medium">VPN Gate</span>
                  <span class="mt-0.5 block text-[11px] text-muted">仅输出已就绪出口中已启用的入站。</span>
                </span>
                <input :checked="vpnAll" name="sub-vpn-all" type="checkbox" class="mt-1 accent-accent" @change="toggleVpnAll(($event.target as HTMLInputElement).checked)" />
              </label>
              <div class="scrollbar-thin mt-3 max-h-64 space-y-1.5 overflow-y-auto border-t border-line pt-3">
                <label v-for="exit in exits" :key="exit.slot" class="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    name="sub-vpn-slot"
                    :value="exit.slot"
                    :checked="vpnAll || slots.has(exit.slot)"
                    :disabled="vpnAll"
                    class="accent-accent"
                    @change="toggleVpnSlot(exit.slot, ($event.target as HTMLInputElement).checked)"
                  />
                  <span class="min-w-0 flex-1 truncate">{{ exitName(exit) }}</span>
                  <StatusBadge :label="exitStatusLabel(exit.status)" :tone="exitStatusTone(exit.status)" size="sm" />
                </label>
                <p v-if="!exits.length" class="text-[11px] text-faint">还没有 VPN Gate 出口。</p>
              </div>
            </div>

            <div class="rounded-lg border border-line p-3">
              <label class="flex items-start justify-between gap-3">
                <span>
                  <span class="block text-sm font-medium">自定义代理</span>
                  <span class="mt-0.5 block text-[11px] text-muted">包括今后新增的第三方 SOCKS5。</span>
                </span>
                <input :checked="thirdAll" name="sub-third-all" type="checkbox" class="mt-1 accent-accent" @change="toggleThirdAll(($event.target as HTMLInputElement).checked)" />
              </label>
              <div class="scrollbar-thin mt-3 max-h-52 space-y-1.5 overflow-y-auto border-t border-line pt-3">
                <label v-for="proxy in thirdProxies" :key="proxy.id" class="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    name="sub-third-proxy"
                    :value="proxy.id"
                    :checked="thirdAll || proxies.has(proxy.id)"
                    :disabled="thirdAll"
                    class="accent-accent"
                    @change="toggleThird(proxy.id, ($event.target as HTMLInputElement).checked)"
                  />
                  <span class="min-w-0 flex-1 truncate">{{ proxy.name || proxy.host }}</span>
                  <span v-if="!proxy.enabled" class="text-[10px] text-faint">已停用</span>
                  <span v-else-if="!proxy.subscribe" class="text-[10px] text-faint">未允许订阅</span>
                  <StatusBadge v-else :label="countryName(proxy.country)" tone="neutral" size="sm" />
                </label>
                <p v-if="!thirdProxies.length" class="text-[11px] text-faint">尚未添加第三方代理。</p>
              </div>
            </div>
          </div>
        </Panel>
      </div>

      <div class="min-w-0 space-y-4">
        <Panel title="扫码订阅" description="二维码由本机服务生成，包含访问口令。">
          <div v-if="qr" class="flex flex-col items-center gap-3 py-2">
            <img :src="qr" alt="订阅二维码" class="size-52 rounded-lg border border-line bg-white p-2" />
            <p class="text-center text-[11px] leading-relaxed text-muted">用客户端扫码导入。链接含访问口令，请妥善保存。</p>
          </div>
          <EmptyState v-else compact title="暂无可生成的二维码" description="至少选择一个订阅来源。">
            <template #icon><QrCode :size="24" /></template>
          </EmptyState>
        </Panel>

        <Panel title="客户端指引">
          <div class="space-y-3 text-xs leading-relaxed text-muted">
            <div>
              <div class="font-medium text-ink">v2rayN</div>
              <p>保持默认格式，复制订阅链接后新建订阅并更新；选择本机直出或已就绪出口。</p>
            </div>
            <div class="border-t border-line pt-3">
              <div class="font-medium text-ink">Clash Verge / Rev</div>
              <p>选择 Clash/Mihomo 格式再复制；本 VPS 直接生成，无需第三方转换。</p>
            </div>
            <div class="border-t border-line pt-3">
              <div class="flex items-center gap-1.5 font-medium text-ink"><ShieldCheck :size="14" />连接检查</div>
              <p>防火墙需放行实际接入端口；只开放管理端口不足以连接代理。</p>
            </div>
            <a
              :href="appUrl('advanced')"
              class="inline-flex items-center gap-1 font-semibold text-accent hover:underline"
              target="_blank"
              rel="noopener"
            >
              打开接入管理 <ExternalLink :size="12" />
            </a>
          </div>
        </Panel>

        <Panel title="订阅状态">
          <dl class="space-y-3 text-xs">
            <div class="flex justify-between gap-2"><dt class="text-faint">就绪出口</dt><dd class="tnum font-medium">{{ readyCount }}</dd></div>
            <div class="flex justify-between gap-2"><dt class="text-faint">订阅口令</dt><dd class="font-mono text-[11px]">{{ sub?.token ? `${sub.token.slice(0, 6)}••••` : "—" }}</dd></div>
            <div class="flex justify-between gap-2"><dt class="text-faint">生成方式</dt><dd>本机服务</dd></div>
          </dl>
          <p class="mt-3 border-t border-line pt-3 text-[11px] leading-relaxed text-faint">
            重置口令会让旧地址立即失效，已订阅的客户端需要重新配置。
          </p>
        </Panel>
      </div>
    </div>

    <ConfirmDialog
      :open="resetOpen"
      title="重置订阅口令"
      description="旧订阅地址会立即失效，已导入的客户端需要重新配置。出口和接入设置不受影响。"
      confirm-label="重置口令"
      :busy="resetMutation.isPending.value"
      @update:open="resetOpen = $event"
      @confirm="resetMutation.mutate()"
    />
  </div>
</template>
