<script setup lang="ts">
import { ref } from "vue";
import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import SchedulerLogPanel from "./scheduling/SchedulerLogPanel.vue";

const workspace = useMediaWorkspaceRoute("scheduling");
const logPanel = ref<{ refresh: () => Promise<boolean> } | null>(null);
const views = [{ key: "logs", label: "调度日志", description: "决策样本审计" }];

async function refreshActive() {
  await workspace.refreshScope();
  await logPanel.value?.refresh();
}
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title" description="审计媒体节点分配决策与调度结果。"
    :views="views.filter(view => workspace.allowedViews.value.includes(view.key))" :active-view="workspace.activeView.value"
    :scope="workspace.scope.value" :nodes="workspace.nodes.value" :status="workspace.status.value"
    :status-text="workspace.statusText.value" :last-success-at="workspace.lastSuccessAt.value"
    :auto-refresh="workspace.autoRefresh.value" :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    :allow-all="false" :requires-node="true"
    @update:active-view="workspace.setActiveView" @update:scope="workspace.setScope"
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="refreshActive" @refresh-scope="workspace.refreshScope"
  >
    <template #logs="{ active }">
      <SchedulerLogPanel ref="logPanel" :active="active" :auto-refresh="workspace.autoRefresh.value" :nodes="workspace.nodes.value" :scope="workspace.scope.value" />
    </template>
  </MediaWorkspaceShell>
</template>
