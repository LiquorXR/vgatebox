<script setup lang="ts">
import { Plus, Trash2 } from "@lucide/vue";
import { useMutation, useQueryClient } from "@tanstack/vue-query";
import { computed, reactive, ref } from "vue";
import { z } from "zod";
import { api, post, queryString } from "@/api/client";
import { keys } from "@/api/queries";
import type { ThirdPartyProxy, ThirdPartyRow, ThirdPartyView } from "@/api/types";
import ConfirmDialog from "@/components/ConfirmDialog.vue";
import DialogShell from "@/components/DialogShell.vue";
import EmptyState from "@/components/EmptyState.vue";
import Panel from "@/components/Panel.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import ToggleSwitch from "@/components/ToggleSwitch.vue";
import UiButton from "@/components/UiButton.vue";
import { useAccessError } from "@/lib/access-error";
import { countryName } from "@/lib/format";
import { checkStatusLabel, checkStatusTone, errorLabel } from "@/lib/status";
import { toast, toastError } from "@/lib/toast";

const props = defineProps<{
  data: ThirdPartyView | null | undefined;
  loading: boolean;
}>();

const queryClient = useQueryClient();
const accessError = useAccessError();
const thirdData = computed(() => props.data);
const thirdLoading = computed(() => props.loading);

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
  country: z
    .string()
    .trim()
    .max(2)
    .regex(/^$|^[A-Za-z]{2}$/, "国家须为两位代码，如 US、JP；也可留空")
    .optional(),
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
  const country = (parsed.data.country ?? "").trim().toUpperCase();
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
  <div class="space-y-4">
    <div class="rounded-xl border border-line bg-surface-2 px-4 py-3 text-xs leading-relaxed text-muted">
      手动添加自己的 SOCKS5，独立加入订阅。流量经本 VPS 转发；供应商账号和密码不会进入客户端订阅。
    </div>

    <Panel flush>
      <template #actions>
        <UiButton size="sm" variant="primary" @click="openThird()"><Plus :size="14" />添加代理</UiButton>
      </template>
      <div v-if="thirdLoading && !thirdData" class="p-4"><SkeletonBlock :lines="5" /></div>
      <div v-else-if="!thirdRows.length" class="p-4">
        <EmptyState title="还没有第三方代理" description="添加固定 SOCKS5 地址后，可以独立检测并选择是否加入订阅。">
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

  <ConfirmDialog
    :open="deleteOpen"
    title="删除第三方代理"
    :description="`将删除 ${deleteRow?.name || deleteRow?.host || '该代理'} 及其接入入口，相关客户端连接会中断。`"
    confirm-label="删除"
    :busy="deleteMutation.isPending.value"
    @update:open="deleteOpen = $event"
    @confirm="deleteRow && deleteMutation.mutate(deleteRow.id)"
  />
</template>
