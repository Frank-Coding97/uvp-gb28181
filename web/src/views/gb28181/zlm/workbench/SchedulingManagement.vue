<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("scheduling");
const views = [
  { key: "strategy", label: "调度策略", description: "新分配生效" },
  { key: "logs", label: "调度日志", description: "决策样本审计" }
];
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="配置新媒体分配策略并审计调度结果；策略切换不会迁移现有流。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))" :active-view="workspace.activeView.value"
    :scope="workspace.scope.value" :nodes="workspace.nodes.value" :status="workspace.status.value"
    :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template #strategy><div class="workspace-pending">正在准备调度策略面板…</div></template>
    <template #logs><div class="workspace-pending">正在准备调度日志面板…</div></template>
  </MediaWorkspaceShell>
</template>

<style scoped>.workspace-pending { display: grid; min-height: 280px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }</style>
