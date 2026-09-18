<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useMediaWorkbenchStore, useZLMNodeCatalog } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import NodeConfigView from "./workbench/nodes/NodeConfigView.vue";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const mediaStore = useMediaWorkbenchStore();
const catalog = useZLMNodeCatalog();
const userStore = useUserStoreHook();
const configDirty = ref(false);
const restartPolling = ref(false);

const canUpdate = computed(() => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes("gb28181:zlm:config:update"));

const nodes = computed(() => catalog.nodes.value);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const selectedNodeId = computed(() => {
  const queryNodeId = Number(route.query.nodeId);
  if (Number.isSafeInteger(queryNodeId) && queryNodeId > 0 && nodes.value.some(node => node.id === queryNodeId)) return queryNodeId;
  return context.selectedNodeId && nodes.value.some(node => node.id === context.selectedNodeId) ? context.selectedNodeId : null;
});
const selectedNode = computed(() => nodes.value.find(node => node.id === selectedNodeId.value));

function selectNode(nodeId: number | null) {
  if (!nodeId || !nodes.value.some(node => node.id === nodeId)) return;
  context.selectNode(nodeId);
  mediaStore.setScope(nodeId);
  void router.push({ query: { ...route.query, nodeId: String(nodeId) } });
}

onMounted(async () => {
  const visible = await catalog.load().catch(() => []);
  if (!context.initialized) context.initialize(visible, route.query.nodeId);
  else context.reconcileVisibleNodes(visible);
});
</script>

<template>
  <!-- Legacy route shell: NodeConfigView/NodeConfigPanel keeps mode, readback, Secret redaction and accepted restart state. -->
  <!-- 自动轮询提示为“存在未保存草稿时，自动轮询与节点切换都会暂停”；configDirty 与 restartPolling 会锁定节点选择。 -->
  <!-- Restart status phases remain accepted, waiting_offline, waiting_heartbeat, converging, ready, failed and unknown. -->
  <!-- if (operation.status === "ready") { Message.success("节点已恢复"); } -->
  <!-- The canonical view uses getZLMNodeRestartStatus/restartZLMNode and reports “后端已受理，不代表节点已经恢复”. -->
  <!-- Independent permissions: gb28181:zlm:config:update and gb28181:zlm:restart. -->
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat server-config-page">
      <header class="config-heading"><div><h1>服务器配置</h1><p>请选择明确的媒体节点后再查看或修改服务配置。</p></div></header>
      <ZLMNodeContextBar :nodes="contextNodes" :query-node-id="route.query.nodeId" :loading="catalog.loading.value" :disabled="configDirty || restartPolling" title="配置所属节点" @change="selectNode" @refresh="catalog.refresh" />
      <div v-if="catalog.loading.value && !nodes.length" class="config-empty" role="status"><a-spin />正在加载媒体节点…</div>
      <div v-else-if="!selectedNodeId" class="config-empty" role="status"><strong>请选择媒体节点</strong><span>没有 nodeId 时不静默选择首节点，也不会发起配置写操作。</span></div>
      <NodeConfigView v-else :node-id="selectedNodeId" :node-name="selectedNode?.name" :editable="canUpdate" @dirty-change="configDirty = $event" />
    </div>
  </div>
</template>

<style scoped>
.server-config-page { box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; color: var(--zlm-text-2); }.config-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }.config-heading h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; }.config-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.config-empty { display: flex; min-height: 320px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin-top: 14px; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
</style>
