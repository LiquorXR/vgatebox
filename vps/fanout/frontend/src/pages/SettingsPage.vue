<script setup lang="ts">
import { Check, RefreshCw, ShieldCheck, SlidersHorizontal } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref, watch } from "vue";
import { api, post } from "@/api/client";
import { keys, useSettingsQuery, useUpdateStatusQuery } from "@/api/queries";
import type { UpdateStatus } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import Panel from "@/components/Panel.vue";
import ProgressBar from "@/components/ProgressBar.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { formatDateTime, formatVersion } from "@/lib/format";
import { themeChoice, setTheme, type ThemeChoice } from "@/lib/theme";
import { toast, toastError } from "@/lib/toast";

const queryClient = useQueryClient();
const { data: settings, isLoading } = useSettingsQuery(true);
const { data: job } = useUpdateStatusQuery(true);

const form = reactive({
  port: 8899,
  listen_addr: "0.0.0.0",
  base_path: "",
  password: "",
  current_password: "",
  residential_only: true,
});
const saving = ref(false);
const checkedAt = ref("");

watch(
  settings,
  (value) => {
    if (!value) return;
    form.port = value.port;
    form.listen_addr = value.listen_addr || "0.0.0.0";
    form.base_path = value.base_path || "";
    form.residential_only = value.residential_only;
    form.password = "";
    form.current_password = "";
  },
  { immediate: true },
);

const newAddress = computed(() => {
  const scheme = window.location.protocol;
  const host = window.location.hostname;
  const port = form.port || settings.value?.port || 8899;
  const path = form.base_path.trim().replace(/\/+$/, "");
  const suffix = window.location.pathname.endsWith("/next/") ? "next/" : "";
  return `${scheme}//${host}:${port}${path}/${suffix}`;
});

const saveMutation = useMutation({
  mutationFn: async () => {
    const port = Number(form.port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("管理端口须为 1–65535");
    if (form.password && form.password.length < 4) throw new Error("新密码至少 4 位");
    if (form.password && !form.current_password.trim()) throw new Error("修改密码需要输入当前密码");
    const body: Record<string, unknown> = {
      port,
      listen_addr: form.listen_addr.trim(),
      base_path: form.base_path.trim(),
      residential_only: form.residential_only,
    };
    if (form.password) {
      body.password = form.password;
      body.current_password = form.current_password;
    }
    return post("api/settings", body);
  },
  onSuccess: () => {
    form.password = "";
    form.current_password = "";
    toast("设置已保存；如端口或路径变化，请使用新地址访问", { tone: "success" });
    void queryClient.invalidateQueries({ queryKey: keys.settings });
    void queryClient.invalidateQueries({ queryKey: keys.console });
  },
  onError: (error) => toastError(error, "保存设置失败"),
});

const updateStatus = ref<UpdateStatus | null>(null);
const checking = ref(false);
const applying = ref(false);

async function checkUpdate() {
  checking.value = true;
  try {
    const value = await api<UpdateStatus>("api/update/check");
    updateStatus.value = value;
    checkedAt.value = new Date().toISOString();
    toast(value.has_update ? `发现新版本 ${value.latest}` : "当前已是最新正式版", { tone: "success" });
  } catch (error) {
    toastError(error, "检查更新失败");
  } finally {
    checking.value = false;
  }
}

async function applyUpdate() {
  if (!updateStatus.value?.has_update) return;
  applying.value = true;
  try {
    await post("api/update/apply", { expected_version: updateStatus.value.latest });
    toast("更新任务已启动；服务重启后可能需要重新登录");
    void queryClient.invalidateQueries({ queryKey: keys.update });
  } catch (error) {
    toastError(error, "启动更新失败");
  } finally {
    applying.value = false;
  }
}

const jobPercent = computed(() => {
  const phase = job.value?.phase || "";
  const map: Record<string, number> = {
    preparing: 15,
    downloading: 35,
    verifying: 55,
    installing: 75,
    restarting: 90,
    done: 100,
  };
  return map[phase] || (job.value?.status === "running" ? 25 : job.value?.status === "done" ? 100 : 0);
});

const jobTone = computed(() => {
  if (job.value?.status === "running") return "info" as const;
  if (job.value?.status === "failed" || job.value?.status === "interrupted") return "bad" as const;
  if (job.value?.status === "done" || job.value?.status === "succeeded") return "ok" as const;
  return "neutral" as const;
});

const jobLabel = computed(() => {
  const labels: Record<string, string> = {
    idle: "空闲",
    running: "进行中",
    done: "已完成",
    succeeded: "已完成",
    failed: "失败",
    interrupted: "已中断",
  };
  return labels[job.value?.status || ""] || job.value?.status || "空闲";
});

const themeOptions: { value: ThemeChoice; label: string }[] = [
  { value: "auto", label: "跟随系统" },
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
];
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="设置" description="管理访问入口、更新、外观和高级接入。">
      <template #actions>
        <UiButton :loading="checking" @click="checkUpdate"><RefreshCw :size="15" />检查更新</UiButton>
      </template>
    </PageHeader>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
      <div class="min-w-0 space-y-4">
        <Panel title="版本与更新" description="更新完整程序并保留配置；服务重启后可能需要重新登录。">
          <div class="space-y-3">
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <span class="text-muted">当前版本</span>
              <b class="font-mono">{{ formatVersion(settings?.version) }}</b>
              <StatusBadge
                v-if="updateStatus"
                :label="updateStatus.has_update ? `可更新至 ${updateStatus.latest}` : '已是最新'"
                :tone="updateStatus.has_update ? 'warn' : 'ok'"
                size="sm"
              />
              <span v-if="checkedAt" class="text-[11px] text-faint">检查于 {{ formatDateTime(checkedAt) }}</span>
            </div>

            <div v-if="job" class="rounded-lg border border-line bg-surface-2 p-3">
              <div class="flex items-center justify-between gap-2 text-xs">
                <span class="font-medium">{{ job.message || "尚无更新任务" }}</span>
                <StatusBadge :label="jobLabel" :tone="jobTone" size="sm" :pulse="job.status === 'running'" />
              </div>
              <ProgressBar v-if="job.status === 'running'" :value="jobPercent" size="sm" class="mt-2" label="更新进度" />
              <p v-if="job.target" class="mt-2 text-[11px] text-faint">目标版本 {{ job.target }} · {{ job.phase || "准备中" }}</p>
            </div>

            <div class="flex flex-wrap gap-2">
              <UiButton v-if="updateStatus?.has_update" variant="primary" :loading="applying" @click="applyUpdate">安装 {{ updateStatus.latest }}</UiButton>
              <UiButton :loading="checking" @click="checkUpdate">重新检查</UiButton>
              <a
                v-if="updateStatus?.url"
                :href="updateStatus.url"
                target="_blank"
                rel="noopener"
                class="inline-flex h-9 items-center gap-1 rounded-lg px-3 text-sm font-medium text-accent hover:bg-surface-3"
              >
                版本记录 <ExternalLink :size="13" />
              </a>
            </div>

            <div v-if="updateStatus && !updateStatus.supported" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
              该版本未提供当前架构的完整包，请使用安装入口更新。
            </div>

            <details v-if="updateStatus?.notes" class="rounded-lg border border-line px-3 py-2">
              <summary class="cursor-pointer text-xs font-medium text-muted">版本说明</summary>
              <pre class="mt-2 whitespace-pre-wrap font-sans text-[11px] leading-relaxed text-muted">{{ updateStatus.notes }}</pre>
            </details>
          </div>
        </Panel>

        <Panel title="管理入口" description="改端口或路径后请使用新地址访问；改密码会使其他会话退出。">
          <div v-if="isLoading && !settings" class="py-3"><SkeletonBlock :lines="4" /></div>
          <form v-else class="space-y-4" @submit.prevent="saveMutation.mutate()">
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="block">
                <span class="mb-1 block text-xs font-medium">管理端口</span>
                <input v-model.number="form.port" name="management-port" type="number" min="1" max="65535" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
              </label>
              <label class="block">
                <span class="mb-1 block text-xs font-medium">监听地址</span>
                <input v-model="form.listen_addr" name="management-listen" required class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
                <span class="mt-1 block text-[11px] text-faint">0.0.0.0 表示所有网卡；127.0.0.1 仅本机。</span>
              </label>
            </div>
            <label class="block">
              <span class="mb-1 block text-xs font-medium">管理路径</span>
              <input v-model="form.base_path" name="management-path" placeholder="留空表示不加前缀" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 font-mono text-sm outline-none focus:border-accent" />
              <span class="mt-1 block text-[11px] text-faint">随机路径可降低被扫描的概率；留空表示不加前缀。</span>
            </label>
            <label class="block">
              <span class="mb-1 block text-xs font-medium">当前密码（修改密码时必填）</span>
              <input v-model="form.current_password" name="current-password" type="password" autocomplete="current-password" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
            </label>
            <label class="block">
              <span class="mb-1 block text-xs font-medium">新密码（留空保留原密码）</span>
              <input v-model="form.password" name="new-password" type="password" autocomplete="new-password" class="h-9 w-full rounded-lg border border-line-strong bg-surface px-3 text-sm outline-none focus:border-accent" />
              <span class="mt-1 block text-[11px] text-faint">至少 4 位；修改后其他已登录会话会退出，当前会话保留。</span>
            </label>
            <label class="flex items-start justify-between gap-4 rounded-lg border border-line p-3">
              <span>
                <span class="block text-sm font-medium">只使用志愿者家宽</span>
                <span class="mt-0.5 block text-[11px] leading-relaxed text-muted">影响自动选点（新建、补齐、重连、换节点），不影响节点池列表和手动指定节点；没有志愿者节点时会等待。</span>
              </span>
              <ToggleSwitch v-model="form.residential_only" label="只使用志愿者家宽" />
            </label>

            <div v-if="form.port !== settings?.port || form.base_path !== (settings?.base_path || '')" class="rounded-lg border border-warn/25 bg-warn-soft px-3 py-2 text-[11px] leading-relaxed text-warn">
              新管理地址为 <span class="break-all font-mono">{{ newAddress }}</span>。保存后请使用新地址访问。
            </div>

            <UiButton type="submit" variant="primary" :loading="saveMutation.isPending.value">保存设置</UiButton>
          </form>
        </Panel>
      </div>

      <div class="min-w-0 space-y-4">
        <Panel title="外观">
          <p class="text-xs text-muted">跟随系统时，会随操作系统的明暗模式自动切换。</p>
          <div class="mt-3 grid grid-cols-3 gap-1 rounded-lg border border-line bg-surface-2 p-1" role="group" aria-label="外观主题">
            <button
              v-for="option in themeOptions"
              :key="option.value"
              type="button"
              class="flex h-8 items-center justify-center gap-1 rounded-md text-xs font-medium transition-colors"
              :class="themeChoice === option.value ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:bg-surface-3'"
              @click="setTheme(option.value)"
            >
              <Check v-if="themeChoice === option.value" :size="12" class="text-accent" />
              {{ option.label }}
            </button>
          </div>
        </Panel>

        <Panel title="关于">
          <div class="space-y-3 text-xs leading-relaxed text-muted">
            <div class="flex items-center gap-2 text-ink"><ShieldCheck :size="16" class="text-accent" /><b>vgatebox</b></div>
            <p>在自己的 VPS 上管理 VPN Gate、第三方 SOCKS5 和本机直出，用一条订阅在 v2rayN、Clash/Mihomo 等客户端使用。</p>
            <p>内置 Xray 提供客户端接入，OpenVPN 建立 VPN Gate 出口；配置和凭据只保存在这台 VPS 上。第三方组件许可见安装包 licenses 目录。</p>
            <div class="flex flex-wrap gap-2 border-t border-line pt-3 text-[11px] text-faint">
              <span class="inline-flex items-center gap-1"><SlidersHorizontal :size="12" />内置 Xray</span>
              <span>·</span>
              <span>单二进制分发</span>
              <span>·</span>
              <span>{{ formatVersion(settings?.version) }}</span>
            </div>
          </div>
        </Panel>

        <Panel title="安全建议">
          <ul class="space-y-2 text-xs leading-relaxed text-muted">
            <li>· 公网管理建议使用 HTTPS 反向代理或 SSH 转发。</li>
            <li>· 管理地址、订阅和备份含私密信息；不要公开分享。</li>
            <li>· 防火墙需放行管理端口和实际接入端口。</li>
            <li>· 重装系统后订阅地址会变化，需要重新导入。</li>
          </ul>
        </Panel>
      </div>
    </div>
  </div>
</template>
