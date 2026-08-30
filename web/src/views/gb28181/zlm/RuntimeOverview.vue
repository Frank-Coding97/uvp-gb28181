<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import type { MediaScope } from "@/store/modules/media-workbench";
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
const scope = computed<MediaScope>(() => selectedNodeId.value ?? "all");

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

function drilldown(view: "streams" | "sessions") {
  void router.push({ path: `/gb28181/zlm/${view}`, query: selectedNodeId.value ? { nodeId: String(selectedNodeId.value) } : {} });
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat legacy-runtime-overview">
      <ZLMNodeContextBar :nodes="nodes" :query-node-id="route.query.nodeId" :loading="nodesLoading" title="运行态节点" @refresh="refreshAll" />
      <div v-if="nodesError && !nodes.length" class="legacy-runtime-error" role="alert">节点目录加载失败，请刷新重试。</div>
      <RuntimeSummaryPanel ref="panel" :active="true" :scope="scope" :node-id="selectedNodeId" @drilldown="drilldown" />
    </div>
  </div>
</template>

<style scoped>
.legacy-runtime-overview { box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; }.legacy-runtime-error { margin: 10px 0; padding: 9px 12px; color: var(--zlm-danger-600); background: var(--zlm-danger-50); border: 1px solid var(--zlm-danger-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
</style>
