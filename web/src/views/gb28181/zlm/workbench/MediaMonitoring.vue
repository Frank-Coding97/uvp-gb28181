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
  { key: "sessions", label: "网络会话", description: "网络连接与资源归属" },
  { key: "viewers", label: "媒体观看者", description: "媒体流观看连接" }
];
const nodeId = computed(() => typeof workspace.scope.value === "number" ? workspace.scope.value : null);

function refreshActivePanel() {
  if (workspace.activeView.value === "streams") streamPanel.value?.refresh();
  if (["sessions", "viewers"].includes(workspace.activeView.value)) sessionPanel.value?.refresh();
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
    :allow-all="false" :requires-node="true"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="refreshAll" @refresh-scope="workspace.refreshScope"
  >
    <template #content>
      <section v-show="workspace.activeView.value === 'streams'" class="media-monitoring-view" data-panel-view="streams">
        <StreamPanel ref="streamPanel" :active="workspace.activeView.value === 'streams'" :scope="workspace.scope.value" :node-id="nodeId" :initial-query="route.query" />
      </section>
      <section v-show="['sessions', 'viewers'].includes(workspace.activeView.value)" class="media-monitoring-view" :data-panel-view="workspace.activeView.value">
        <NetworkSessionPanel
          ref="sessionPanel"
          :active="['sessions', 'viewers'].includes(workspace.activeView.value)"
          :view="workspace.activeView.value === 'viewers' ? 'viewers' : 'network'"
          :scope="workspace.scope.value"
          :node-id="nodeId"
          :initial-query="route.query"
        />
      </section>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.media-monitoring-view { box-sizing: border-box; width: 100%; min-height: 100%; }
</style>
