<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import type { MediaScope } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";

import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import StreamPanel from "./workbench/monitoring/StreamPanel.vue";

const route = useRoute();
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

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat legacy-stream-management">
      <ZLMNodeContextBar
        :nodes="nodes"
        :query-node-id="route.query.nodeId"
        :loading="nodesLoading"
        title="流所在节点"
        @refresh="refreshAll"
      />
      <div v-if="nodesError && !nodes.length" class="legacy-stream-error" role="alert">节点目录加载失败，请刷新重试。</div>
      <StreamPanel ref="panel" :active="true" :scope="scope" :node-id="selectedNodeId" :initial-query="route.query" />
    </div>
  </div>
</template>

<style scoped>
/* ⛔ 必须做成 flex 列：面板（StreamPanel）靠 `flex: 1 + min-height: 0`
   把可用高度算出来，表格才能自己滚；这里若还是普通块级 + overflow:auto，
   面板高度退回 auto，竖向滚动条又回到这一层（表头跟着页面滚走）。 */
.legacy-stream-management {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  padding: 4px 8px 24px;
  overflow: hidden;
}
.legacy-stream-error {
  flex: none;
  padding: 9px 12px;
  margin: 10px 0;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
  border: 1px solid var(--zlm-danger-500);
  border-radius: var(--zlm-radius-md);
}
</style>
