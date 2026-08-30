<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("monitoring");
const views = [
  { key: "streams", label: "流媒体", description: "在线媒体与录制态" },
  { key: "sessions", label: "网络会话", description: "连接与资源归属" }
];
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
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template #streams><div class="workspace-pending">正在准备流媒体面板…</div></template>
    <template #sessions><div class="workspace-pending">正在准备网络会话面板…</div></template>
  </MediaWorkspaceShell>
</template>

<style scoped>.workspace-pending { display: grid; min-height: 280px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }</style>
