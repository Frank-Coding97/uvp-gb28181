<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import { useZLMContextStore } from "@/store/modules/zlm-context";

import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import RuntimeSummaryPanel from "./workbench/monitoring/RuntimeSummaryPanel.vue";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const panel = ref<{ refresh: () => void } | null>(null);
const selectedNodeId = computed(() => context.selectedNodeId);

async function loadNodes() {
  nodesLoading.value = true;
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
    nodes.value = response.data?.list ?? [];
    nodesError.value = null;
  } catch (error) {
    nodesError.value = error;
  } finally {
    nodesLoading.value = false;
  }
}

function refreshAll() {
  void loadNodes();
  panel.value?.refresh();
}

function updateScope(nodeId: number | null) {
  const query = { ...route.query };
  if (nodeId === null) delete query.nodeId;
  else query.nodeId = String(nodeId);
  void router.replace({ path: route.path, query });
}

function drilldown(view: "streams" | "sessions") {
  void router.push({ path: `/gb28181/zlm/${view}`, query: selectedNodeId.value ? { nodeId: String(selectedNodeId.value) } : {} });
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat merged-media-overview">
      <ZLMNodeContextBar
        :nodes="nodes"
        :query-node-id="route.query.nodeId"
        :loading="nodesLoading"
        :fallback-first="true"
        :minimal="true"
        @change="updateScope"
        @refresh="refreshAll"
      />
      <div v-if="nodesError && !nodes.length" class="merged-media-overview__error" role="alert">节点目录加载失败，请刷新重试。</div>
      <RuntimeSummaryPanel
        ref="panel"
        :active="true"
        :node-id="selectedNodeId"
        @drilldown="drilldown"
      />
    </div>
  </div>
</template>

<style scoped>
.merged-media-overview { box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; }
.merged-media-overview__error { margin: 10px 0; padding: 9px 12px; color: var(--zlm-danger-600); background: var(--zlm-danger-50); border: 1px solid var(--zlm-danger-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
</style>
