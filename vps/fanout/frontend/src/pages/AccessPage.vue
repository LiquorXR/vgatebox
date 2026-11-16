<script setup lang="ts">
import { computed, ref } from "vue";
import { useExitsQuery, useSocksLinksQuery, useSstpQuery, useThirdPartyQuery } from "@/api/queries";
import AdvancedAccessPanel from "@/components/AdvancedAccessPanel.vue";
import PageHeader from "@/components/PageHeader.vue";
import SectionTabs from "@/components/SectionTabs.vue";
import SkeletonBlock from "@/components/SkeletonBlock.vue";
import SocksAccessTab from "@/pages/access/SocksAccessTab.vue";
import SstpAccessTab from "@/pages/access/SstpAccessTab.vue";
import ThirdPartyAccessTab from "@/pages/access/ThirdPartyAccessTab.vue";

const tab = ref("socks");
const { data: exitsView, isLoading: exitsLoading } = useExitsQuery(true);
const exits = computed(() => exitsView.value?.exits || []);
const hostDirect = computed(() => exitsView.value?.host_direct || []);
const direct = computed(() => exitsView.value?.direct || []);
const inboundCount = computed(
  () =>
    exits.value.reduce((total, exit) => total + (exit.inbounds?.length || 0), 0) +
    hostDirect.value.length +
    direct.value.length,
);

const { data: socksData } = useSocksLinksQuery(computed(() => tab.value === "socks"));
const { data: sstpData, isLoading: sstpLoading } = useSstpQuery(computed(() => tab.value === "sstp"));
const { data: thirdData, isLoading: thirdLoading } = useThirdPartyQuery(computed(() => tab.value === "third"));

const tabs = computed(() => [
  { key: "socks", label: "VPS SOCKS5", count: exits.value.filter((exit) => exit.public_socks?.enable).length },
  { key: "sstp", label: "原始 SSTP", count: sstpData.value?.nodes.length },
  { key: "third", label: "第三方 SOCKS5", count: thirdData.value?.proxies.length },
  { key: "advanced", label: "高级接入", count: inboundCount.value },
]);
</script>

<template>
  <div class="space-y-5">
    <PageHeader title="接入" description="管理服务器代理与原始连接，各自有明确的用途。" />

    <div class="flex flex-wrap items-center gap-3">
      <SectionTabs v-model="tab" :items="tabs" />
    </div>

    <div v-if="exitsLoading && !exitsView" class="rounded-xl border border-line bg-surface p-4"><SkeletonBlock :lines="5" /></div>

    <template v-else-if="tab === 'socks'">
      <SocksAccessTab :exits="exits" :links="socksData?.links || []" :public-ip="exitsView?.public_ip || ''" />
    </template>

    <template v-else-if="tab === 'sstp'">
      <SstpAccessTab :data="sstpData" :loading="sstpLoading" />
    </template>

    <template v-else-if="tab === 'advanced'">
      <AdvancedAccessPanel :exits="exits" />
    </template>

    <template v-else-if="tab === 'third'">
      <ThirdPartyAccessTab :data="thirdData" :loading="thirdLoading" />
    </template>
  </div>
</template>
