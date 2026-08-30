<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("overview");
const views = [{ key: "overview", label: "全局态势", description: "节点与媒体健康" }];
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
    @refresh="workspace.refreshScope"
    @refresh-scope="workspace.refreshScope"
  >
    <template #overview><div class="workspace-pending" role="status">正在准备媒体态势面板…</div></template>
  </MediaWorkspaceShell>
</template>

<style scoped>.workspace-pending { display: grid; min-height: 280px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }</style>
