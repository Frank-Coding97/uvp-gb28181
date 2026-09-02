<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import NodeListPanel from "./nodes/NodeListPanel.vue";

const workspace = useMediaWorkspaceRoute("nodes");
const views = [{ key: "list", label: "节点列表", description: "生命周期与容量" }];
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="管理媒体节点生命周期、连接信息和容量边界，详情页承载运行态与服务配置。"
    :views="views" :active-view="workspace.activeView.value" :scope="workspace.scope.value" :nodes="workspace.nodes.value"
    :status="workspace.status.value" :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :allow-all="false" :requires-node="true"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template #list>
      <NodeListPanel
        :nodes="workspace.nodes.value"
        :scope="workspace.scope.value"
        :loading="workspace.scopeLoading.value"
        :error="workspace.scopeError.value"
        :auto-refresh="workspace.autoRefresh.value"
        @refresh="workspace.refreshScope"
      />
    </template>
  </MediaWorkspaceShell>
</template>
