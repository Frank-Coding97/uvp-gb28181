<template>
  <a-badge :count="store.activeCount" :max-count="9" dot>
    <a-tooltip content="下载任务">
      <a-button
        type="text"
        size="mini"
        class="icon_btn"
        aria-label="下载任务"
        :aria-expanded="visible"
        @click="visible = true"
      >
        <template #icon><Download :size="18" /></template>
      </a-button>
    </a-tooltip>
  </a-badge>

  <a-drawer v-model:visible="visible" width="min(440px, 100vw)" :footer="false" unmount-on-close class="recording-download-drawer">
    <template #title>下载任务</template>
    <div class="recording-download-toolbar">
      <span>{{ store.activeCount ? `${store.activeCount} 个进行中` : "暂无进行中的下载" }}</span>
      <a-button v-if="store.tasks.length" type="text" size="small" @click="clearTerminal">清理已结束</a-button>
    </div>
    <a-empty v-if="!store.tasks.length" description="暂无下载任务" />
    <div v-else class="recording-download-list">
      <section v-for="task in store.tasks" :key="task.taskId" class="recording-download-item">
        <div class="recording-download-item__main">
          <strong :title="task.fileName">{{ task.fileName }}</strong>
          <span>{{ statusText(task.status) }}</span>
          <a-progress v-if="task.totalBytes" :percent="progress(task)" size="small" :show-text="false" />
          <small>{{ progressLabel(task) }}</small>
        </div>
        <div class="recording-download-item__actions">
          <a-tooltip v-if="canRetry(task.status)" :content="task.status === 'ready' ? '重新触发浏览器下载' : '从零重新下载'">
            <a-button type="text" size="mini" aria-label="重新下载" @click="retry(task.taskId)"><template #icon><RefreshCw :size="16" /></template></a-button>
          </a-tooltip>
          <a-tooltip v-if="canCancel(task.status)" content="取消下载">
            <a-button type="text" size="mini" status="danger" aria-label="取消下载" @click="cancel(task.taskId)"><template #icon><X :size="16" /></template></a-button>
          </a-tooltip>
        </div>
      </section>
    </div>
  </a-drawer>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { Download, RefreshCw, X } from "@lucide/vue";
import { useRecordingDownloadStore, type RecordingDownloadItem } from "@/store/modules/recording-downloads";
import { recordingDownloadCoordinator } from "@/views/gb28181/cloud-recordings/recordingDownloadService";

const visible = ref(false);
const store = useRecordingDownloadStore();
const activeStatuses = new Set<RecordingDownloadItem["status"]>(["queued", "ready", "streaming"]);
const terminalStatuses = new Set<RecordingDownloadItem["status"]>(["completed", "failed", "cancelled", "expired"]);

function progress(task: RecordingDownloadItem) {
  return Math.min(100, Math.round((task.bytesSent / task.totalBytes!) * 100));
}

function progressLabel(task: RecordingDownloadItem) {
  if (!task.totalBytes) return task.status === "ready" ? "等待浏览器开始下载" : `${task.bytesSent} B`;
  return `${progress(task)}% · ${task.bytesSent} / ${task.totalBytes} B`;
}

function statusText(status: RecordingDownloadItem["status"]) {
  return { queued: "排队中", ready: "等待浏览器开始", streaming: "下载中", completed: "已完成", failed: "下载失败", cancelled: "已取消", expired: "已过期" }[status];
}

function canCancel(status: RecordingDownloadItem["status"]) { return activeStatuses.has(status); }
function canRetry(status: RecordingDownloadItem["status"]) { return status === "ready" || terminalStatuses.has(status); }
async function cancel(taskId: string) { await Promise.resolve(recordingDownloadCoordinator.cancel(taskId)).catch(() => undefined); }
async function retry(taskId: string) { await Promise.resolve(recordingDownloadCoordinator.retry(taskId)).catch(() => undefined); }
function clearTerminal() { recordingDownloadCoordinator.clearTerminal(); }
</script>

<style scoped lang="scss">
.recording-download-toolbar { display: flex; align-items: center; justify-content: space-between; padding-bottom: 10px; color: var(--uvp-text-secondary); font-size: 12px; border-bottom: 1px solid var(--uvp-border); }
.recording-download-list { display: flex; flex-direction: column; }
.recording-download-item { display: flex; gap: 10px; align-items: flex-start; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid var(--uvp-border); }
.recording-download-item__main { display: grid; flex: 1; min-width: 0; gap: 4px; }
.recording-download-item__main strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.recording-download-item__main span, .recording-download-item__main small { color: var(--uvp-text-secondary); font-size: 12px; }
.recording-download-item__actions { display: flex; flex: 0 0 auto; }
@media (max-width: 768px) { .recording-download-item__actions :deep(.arco-btn) { width: 44px; min-height: 44px; } }
</style>
