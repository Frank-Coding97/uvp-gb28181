<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";

import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";
import RecordingFilesPanel from "./recording/RecordingFilesPanel.vue";
import RecordingPlansPanel from "./recording/RecordingPlansPanel.vue";
import RecordingSummaryStrip from "./recording/RecordingSummaryStrip.vue";
import RecordingTasksPanel from "./recording/RecordingTasksPanel.vue";
import { useMediaWorkspaceRoute } from "./useMediaWorkspaceRoute";

const workspace = useMediaWorkspaceRoute("recordings");
const route = useRoute();
const filesPanel = ref<{ refresh: () => void } | null>(null);
const tasksPanel = ref<{ refresh: () => void } | null>(null);
const plansPanel = ref<{ refresh: () => void } | null>(null);
const filesTotal = ref<number | null>(null);
const activeTotal = ref<number | null>(null);
const enabledPlanTotal = ref<number | null>(null);
const abnormalChannelTotal = ref<number | null>(null);
const views = [
  { key: "files", label: "录像文件", description: "检索、播放与下载" },
  { key: "tasks", label: "录制任务", description: "活动会话与异常" },
  { key: "plans", label: "录像计划", description: "周计划与分配" }
];

const scopedNodeId = computed(() => typeof workspace.scope.value === "number" ? workspace.scope.value : null);
const summaryItems = computed(() => [
  { key: "files", label: "查询文件", value: filesTotal.value, note: "当前文件筛选结果" },
  { key: "active", label: "正在录像", value: activeTotal.value, note: scopedNodeId.value ? `节点 #${scopedNodeId.value}` : "全部可见节点", tone: "success" as const },
  { key: "plans", label: "启用计划", value: enabledPlanTotal.value, note: "全部可见计划" },
  { key: "abnormal", label: "当前计划异常", value: abnormalChannelTotal.value, note: "最近加载的计划执行状态", tone: abnormalChannelTotal.value && abnormalChannelTotal.value > 0 ? "warning" as const : "default" as const }
]);

function updateFileStats(value: { filesTotal?: number | null; activeTotal?: number | null }) {
  if (value.filesTotal !== undefined) filesTotal.value = value.filesTotal;
  if (value.activeTotal !== undefined) activeTotal.value = value.activeTotal;
}

function updatePlanStats(value: { enabledPlanTotal?: number | null; abnormalChannelTotal?: number | null }) {
  if (value.enabledPlanTotal !== undefined) enabledPlanTotal.value = value.enabledPlanTotal;
  if (value.abnormalChannelTotal !== undefined) abnormalChannelTotal.value = value.abnormalChannelTotal;
}

function refreshActivePanel() {
  if (workspace.activeView.value === "files") filesPanel.value?.refresh();
  else if (workspace.activeView.value === "tasks") tasksPanel.value?.refresh();
  else if (workspace.activeView.value === "plans") plansPanel.value?.refresh();
}

function refreshAll() {
  void workspace.refreshScope();
  refreshActivePanel();
}
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
    @update:auto-refresh="workspace.autoRefresh.value = $event" @refresh="refreshAll" @refresh-scope="workspace.refreshScope"
  >
    <template #files>
      <div class="recording-center-view">
        <RecordingSummaryStrip :items="summaryItems" />
        <RecordingFilesPanel
          ref="filesPanel"
          :active="workspace.activeView.value === 'files'"
          :auto-refresh="workspace.autoRefresh.value"
          :node-id="scopedNodeId"
          :keyword="route.query.stream"
          @stats="updateFileStats"
        />
      </div>
    </template>
    <template #tasks>
      <div class="recording-center-view">
        <RecordingSummaryStrip :items="summaryItems" />
        <RecordingTasksPanel
          ref="tasksPanel"
          :active="workspace.activeView.value === 'tasks'"
          :auto-refresh="workspace.autoRefresh.value"
          :node-id="scopedNodeId"
          @stats="updateFileStats"
        />
      </div>
    </template>
    <template #plans>
      <div class="recording-center-view">
        <RecordingSummaryStrip :items="summaryItems" />
        <RecordingPlansPanel
          ref="plansPanel"
          :active="workspace.activeView.value === 'plans'"
          :stream="route.query.stream"
          :node-id="scopedNodeId"
          @stats="updatePlanStats"
        />
      </div>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.recording-center-view { box-sizing: border-box; display: flex; width: 100%; height: 100%; min-height: 0; flex-direction: column; gap: 10px; overflow: hidden; }
.recording-center-view :deep(.cloud-recordings-page),
.recording-center-view :deep(.recording-schedules-page),
.recording-center-view :deep(.recording-plans-denied) { height: auto; min-height: 0; flex: 1; }
</style>
