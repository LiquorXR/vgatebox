<script setup lang="ts">
import { Search } from "@lucide/vue";
import { ipTypeLabels } from "@/lib/status";

defineProps<{
  countries: [string, string][];
}>();

const search = defineModel<string>("search", { required: true });
const countryFilter = defineModel<string>("countryFilter", { required: true });
const statusFilter = defineModel<string>("statusFilter", { required: true });
const typeFilter = defineModel<string>("typeFilter", { required: true });
</script>

<template>
  <div class="flex flex-wrap items-end gap-2">
    <label class="relative min-w-[220px] flex-1">
      <Search :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-faint" />
      <input v-model="search" name="node-search" type="search" autocomplete="off" placeholder="搜索 IP、主机名、ASN 或国家" class="h-9 w-full rounded-lg border border-line-strong bg-surface pl-8 pr-3 text-xs outline-none focus:border-accent" />
    </label>
    <select v-model="countryFilter" name="node-country" class="h-9 min-w-36 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选来源国家">
      <option value="">所有来源国家</option>
      <option v-for="[code, name] in countries" :key="code" :value="code">{{ name }}（{{ code }}）</option>
    </select>
    <select v-model="statusFilter" name="node-status" class="h-9 min-w-32 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选检测状态">
      <option value="all">所有状态</option>
      <option value="passed">通过</option>
      <option value="failed">未通过</option>
      <option value="not_tested">未检测 / 过期</option>
    </select>
    <select v-model="typeFilter" name="node-type" class="h-9 min-w-32 rounded-lg border border-line-strong bg-surface px-2.5 text-xs outline-none focus:border-accent" aria-label="筛选 IP 类型">
      <option value="all">所有 IP 类型</option>
      <option v-for="(label, type) in ipTypeLabels" :key="type" :value="type">{{ label }}</option>
    </select>
  </div>
</template>
