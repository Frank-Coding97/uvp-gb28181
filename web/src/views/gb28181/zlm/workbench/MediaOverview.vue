<script setup lang="ts">
import { ref } from "vue";

import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import MediaOverviewPanel from "./overview/MediaOverviewPanel.vue";

const workspace = useMediaWorkspaceRoute("overview");
const overviewPanel = ref<InstanceType<typeof MediaOverviewPanel> | null>(null);
const views = [{ key: "overview", label: "全局态势", description: "节点与媒体健康" }];

function refresh() {
  void workspace.refreshScope();
  overviewPanel.value?.refresh();
}
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title"
    description="聚合所有可见媒体节点、在线流与运行风险，快速判断集群是否健康。"
    :views="views"
    :active-view="workspace.activeView.value"
    :scope="workspace.scope.value"
    :nodes="workspace.nodes.value"
    :status="workspace.status.value"
    :status-text="workspace.statusText.value"
    :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value"
    :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView"
    @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event"
    @refresh="refresh"
    @refresh-scope="workspace.refreshScope"
  >
    <template #overview>
      <MediaOverviewPanel ref="overviewPanel" :active="workspace.activeView.value === 'overview'" :auto-refresh="workspace.autoRefresh.value" />
    </template>
  </MediaWorkspaceShell>
</template>
