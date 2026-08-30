<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";

import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import NetworkSessionPanel from "./monitoring/NetworkSessionPanel.vue";
import StreamPanel from "./monitoring/StreamPanel.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("monitoring");
const route = useRoute();
const streamPanel = ref<{ refresh: () => void } | null>(null);
const sessionPanel = ref<{ refresh: () => void } | null>(null);
const views = [
  { key: "streams", label: "流媒体", description: "在线媒体与录制态" },
  { key: "sessions", label: "网络会话", description: "连接与资源归属" }
];
const nodeId = computed(() => typeof workspace.scope.value === "number" ? workspace.scope.value : null);

function refreshActivePanel() {
  if (workspace.activeView.value === "streams") streamPanel.value?.refresh();
  if (workspace.activeView.value === "sessions") sessionPanel.value?.refresh();
}

function refreshAll() {
  void workspace.refreshScope();
  refreshActivePanel();
}

</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="按节点范围巡检在线媒体与网络会话，定位异常并安全处置。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))" :active-view="workspace.activeView.value"
    :scope="workspace.scope.value" :nodes="workspace.nodes.value" :status="workspace.status.value"
    :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="refreshAll" @refresh-scope="workspace.refreshScope"
  >
    <template #streams>
      <StreamPanel ref="streamPanel" :active="workspace.activeView.value === 'streams'" :scope="workspace.scope.value" :node-id="nodeId" :initial-query="route.query" />
    </template>
    <template #sessions>
      <NetworkSessionPanel ref="sessionPanel" :active="workspace.activeView.value === 'sessions'" :scope="workspace.scope.value" :node-id="nodeId" :initial-query="route.query" />
    </template>
  </MediaWorkspaceShell>
</template>
