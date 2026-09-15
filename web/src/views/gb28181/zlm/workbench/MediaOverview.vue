<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";

import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";
import RuntimeSummaryPanel from "./monitoring/RuntimeSummaryPanel.vue";

const workspace = useMediaWorkspaceRoute("overview");
const router = useRouter();
const nodeId = computed(() => typeof workspace.scope.value === "number" ? workspace.scope.value : null);
const views = [{ key: "overview", label: "节点运行态", description: "负载、吞吐与对象状态" }];

function drilldown(view: "streams" | "sessions") {
  void router.push({
    path: "/media/monitoring",
    query: { view, ...(nodeId.value === null ? {} : { nodeId: String(nodeId.value) }) }
  });
}
</script>

<template>
  <MediaWorkspaceShell
    :title="workspace.definition.title"
    description="切换具体 ZL 节点，查看实时负载、媒体吞吐与运行风险。"
    :views="views"
    :active-view="workspace.activeView.value"
    :scope="workspace.scope.value"
    :nodes="workspace.nodes.value"
    :status="workspace.status.value"
    :status-text="workspace.statusText.value"
    :last-success-at="workspace.lastSuccessAt.value"
    :show-toolbar-actions="false"
    :scope-loading="workspace.scopeLoading.value"
    :scope-error="workspace.scopeError.value ? '节点目录刷新失败' : ''"
    :allow-all="false"
    :requires-node="true"
    @update:active-view="workspace.setActiveView"
    @update:scope="workspace.setScope"
  >
    <template #overview>
      <RuntimeSummaryPanel
        :active="workspace.activeView.value === 'overview'"
        :node-id="nodeId"
        @drilldown="drilldown"
      />
    </template>
  </MediaWorkspaceShell>
</template>
