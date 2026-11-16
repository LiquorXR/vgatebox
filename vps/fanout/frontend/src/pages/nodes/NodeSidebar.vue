<script setup lang="ts">
import type { CheckSettingsView, ConsoleSummary, IPTypeCacheView } from "@/api/types";
import Panel from "@/components/Panel.vue";
import UiButton from "@/components/UiButton.vue";
import { postRefreshCheckLabel, scheduleLabel } from "@/lib/automation";
import { formatDateTime, formatHitRate } from "@/lib/format";

defineProps<{
  summary: ConsoleSummary | null | undefined;
  filteredCount: number;
  selectedCount: number;
  sourceText: string;
  checkSettings: CheckSettingsView | undefined;
  checkCycleMinutes: number;
  ipTypeCache: IPTypeCacheView | undefined;
  clearingCache: boolean;
}>();

const emit = defineEmits<{
  "clear-cache": [];
  "clear-selection": [];
}>();
</script>

<template>
  <aside class="min-w-0 space-y-4">
    <Panel title="来源与任务">
      <dl class="space-y-3 text-xs">
        <div><dt class="text-faint">来源</dt><dd class="mt-0.5 leading-relaxed text-ink">{{ sourceText }}</dd></div>
        <div><dt class="text-faint">来源刷新</dt><dd class="mt-0.5 text-ink">{{ scheduleLabel(summary?.automation, "refresh") }}</dd></div>
        <div><dt class="text-faint">候选检测</dt><dd class="mt-0.5 text-ink">{{ postRefreshCheckLabel(summary?.automation) }}</dd></div>
        <div><dt class="text-faint">结果有效</dt><dd class="mt-0.5 text-ink">{{ checkSettings?.check_result_ttl_effective_minutes ? `${checkSettings.check_result_ttl_effective_minutes} 分钟` : "—" }} · 超期显示已过期</dd></div>
        <div><dt class="text-faint">排序观察有效</dt><dd class="mt-0.5 text-ink">{{ checkSettings?.observation_ttl_effective_minutes ? `${checkSettings.observation_ttl_effective_minutes} 分钟` : "—" }}</dd></div>
        <div v-if="checkCycleMinutes"><dt class="text-faint">单轮检测约</dt><dd class="mt-0.5 text-ink">{{ checkCycleMinutes }} 分钟</dd></div>
        <div v-if="ipTypeCache"><dt class="text-faint" title="命中与检测计数为滑动窗口，旧数据自然过期；重启或手动清除归零；有效条目落盘持久化；由出口建连（自动/手动、重连）与节点池候选检测共用">IP 类型缓存（出口 + 节点池）</dt><dd class="mt-0.5 text-ink"><span class="block">缓存命中 {{ ipTypeCache.hits }} 次 · 发起检测 {{ ipTypeCache.queries }} 次 · 命中率 {{ formatHitRate(ipTypeCache.hits, ipTypeCache.queries) }}</span><span class="block">有效条目 {{ ipTypeCache.entries }} 条 · 缓存有效期 {{ checkSettings?.ip_type_ttl_hours ? `${checkSettings.ip_type_ttl_hours} 小时` : "—" }}</span></dd></div>
        <div><dt class="text-faint">上次获取</dt><dd class="mt-0.5 text-ink">{{ formatDateTime(summary?.pool_generated_at) }}</dd></div>
      </dl>
      <UiButton v-if="ipTypeCache?.entries" size="sm" variant="ghost" class="mt-3" :loading="clearingCache" @click="emit('clear-cache')">清除 IP 类型缓存</UiButton>
      <p class="mt-3 rounded-lg border border-line px-3 py-2 text-[11px] leading-relaxed text-faint">
        结果有效期固定 30 分钟，超期显示“已过期”；每次刷新成功后自动全量检测一次覆盖。
      </p>
      <p class="mt-3 border-t border-line pt-3 text-[11px] leading-relaxed text-faint">
        所有获取、镜像校验和节点检测都由本 VPS 执行；检测通过不等于会永久在线。
      </p>
    </Panel>

    <Panel title="筛选结果">
      <div class="grid grid-cols-2 gap-3 text-xs">
        <div><div class="text-[11px] text-faint">当前结果</div><div class="tnum mt-0.5 text-lg font-semibold">{{ filteredCount }}</div></div>
        <div><div class="text-[11px] text-faint">已选择</div><div class="tnum mt-0.5 text-lg font-semibold">{{ selectedCount }}</div></div>
      </div>
      <UiButton v-if="selectedCount" size="sm" variant="ghost" class="mt-3" @click="emit('clear-selection')">清空选择</UiButton>
    </Panel>
  </aside>
</template>
