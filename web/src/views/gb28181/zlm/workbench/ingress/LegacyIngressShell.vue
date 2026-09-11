<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import type { MediaScope } from "@/store/modules/media-workbench";
import ZLMNodeContextBar from "../../components/ZLMNodeContextBar.vue";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const nodes = ref<ZLMNode[]>([]);
const loading = ref(false);
const error = ref<unknown>(null);
const refreshKey = ref(0);

const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const scope = computed<MediaScope>(() => context.selectedNodeId ?? "all");
const selectedNodeLoading = computed(() => loading.value || !context.initialized);

watch(context.selectedNodeId, (nextNodeId, previousNodeId) => {
  if (nextNodeId !== null && previousNodeId !== null && route.query.nodeId !== String(nextNodeId)) {
    void router.replace({ query: { ...route.query, nodeId: String(nextNodeId) } });
  }
});

async function refreshNodes() {
  loading.value = true;
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
    nodes.value = response.data?.list ?? [];
    if (!context.initialized) context.initialize(contextNodes.value, route.query.nodeId);
    else context.reconcileVisibleNodes(contextNodes.value);
    error.value = null;
    refreshKey.value += 1;
  } catch (cause) {
    error.value = cause;
  } finally {
    loading.value = false;
  }
}

onMounted(refreshNodes);
</script>

<template>
  <div class="snow-fill legacy-ingress-shell">
    <ZLMNodeContextBar
      :nodes="contextNodes"
      :query-node-id="route.query.nodeId"
      :loading="selectedNodeLoading"
      :disabled="false"
      @refresh="refreshNodes"
    />
    <div v-if="error && !nodes.length" class="legacy-ingress-shell__error" role="alert">
      节点目录加载失败，请刷新后重试。
    </div>
    <slot :active="true" :scope="scope" :nodes="nodes" :refresh-key="refreshKey" />
  </div>
</template>

<style scoped>
.legacy-ingress-shell { display: flex; width: 100%; height: 100%; min-height: 0; flex-direction: column; gap: 12px; padding: 4px 8px 16px; overflow: auto; color: var(--zlm-text-2); box-sizing: border-box; }
.legacy-ingress-shell__error { padding: 10px 12px; color: var(--zlm-danger-600); background: var(--zlm-danger-50); border: 1px solid var(--zlm-danger-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
</style>
