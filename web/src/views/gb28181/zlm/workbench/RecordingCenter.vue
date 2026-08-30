<script setup lang="ts">
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("recordings");
const views = [
  { key: "files", label: "录像文件", description: "检索、播放与下载" },
  { key: "tasks", label: "录制任务", description: "活动会话与异常" },
  { key: "plans", label: "录像计划", description: "周计划与分配" }
];
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="从计划到任务再到文件统一管理录像生命周期，状态与容量口径保持真实。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))" :active-view="workspace.activeView.value"
    :scope="workspace.scope.value" :nodes="workspace.nodes.value" :status="workspace.status.value"
    :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="workspace.refreshScope" @refresh-scope="workspace.refreshScope"
  >
    <template v-for="view in views" :key="view.key" #[view.key]><div class="workspace-pending">正在准备{{ view.label }}面板…</div></template>
  </MediaWorkspaceShell>
</template>

<style scoped>.workspace-pending { display: grid; min-height: 280px; place-items: center; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }</style>
