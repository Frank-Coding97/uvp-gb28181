<script setup lang="ts">
import { ref } from "vue";
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import SchedulerLogPanel from "./scheduling/SchedulerLogPanel.vue";
import SchedulerStrategyPanel from "./scheduling/SchedulerStrategyPanel.vue";

const workspace = useMediaWorkspaceRoute("scheduling");
const strategyPanel = ref<{ refresh: () => Promise<boolean> } | null>(null);
const logPanel = ref<{ refresh: () => Promise<boolean> } | null>(null);
const views = [
  { key: "strategy", label: "调度策略", description: "新分配生效" },
  { key: "logs", label: "调度日志", description: "决策样本审计" }
];

async function refreshActive() {
  await workspace.refreshScope();
  if (workspace.activeView.value === "strategy") await strategyPanel.value?.refresh();
  else if (workspace.activeView.value === "logs") await logPanel.value?.refresh();
}
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
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="refreshActive" @refresh-scope="workspace.refreshScope"
  >
    <template #strategy="{ active }">
      <SchedulerStrategyPanel ref="strategyPanel" :active="active" />
    </template>
    <template #logs="{ active }">
      <SchedulerLogPanel ref="logPanel" :active="active" :auto-refresh="workspace.autoRefresh.value" :nodes="workspace.nodes.value" />
    </template>
  </MediaWorkspaceShell>
</template>
