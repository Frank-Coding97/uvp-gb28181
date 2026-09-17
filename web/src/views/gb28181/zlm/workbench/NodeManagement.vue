<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import NodeListPanel from "./nodes/NodeListPanel.vue";
import SchedulerStrategyPanel from "./scheduling/SchedulerStrategyPanel.vue";

const workspace = useMediaWorkspaceRoute("nodes");
const views = [{ key: "list", label: "节点列表", description: "生命周期与容量" }];
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="管理媒体节点生命周期、连接信息、容量边界和服务配置。"
    :views="views" :active-view="workspace.activeView.value" scope="all" :nodes="workspace.nodes.value"
    :status="workspace.status.value" :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :show-scope="false" :allow-all="false" :requires-node="true"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template #list>
      <div class="node-management-content">
        <SchedulerStrategyPanel />
        <NodeListPanel
          :nodes="workspace.nodes.value"
          scope="all"
          :loading="workspace.scopeLoading.value"
          :error="workspace.scopeError.value"
          :auto-refresh="workspace.autoRefresh.value"
          @refresh="workspace.refreshScope"
        />
      </div>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.node-management-content {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 16px;
}
</style>
