import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { RecordingDownloadStatus, RecordingDownloadTask } from "@/views/gb28181/cloud-recordings/api";

export interface RecordingDownloadItem extends RecordingDownloadTask {
  fileName: string;
}

const activeStatuses = new Set<RecordingDownloadStatus>(["queued", "ready", "streaming"]);

// Download credentials and URLs deliberately never enter Pinia or persistent storage.
export const useRecordingDownloadStore = defineStore("recording-downloads", () => {
  const tasks = ref<RecordingDownloadItem[]>([]);
  const activeCount = computed(() => tasks.value.filter(task => activeStatuses.has(task.status)).length);

  function upsert(task: RecordingDownloadItem) {
    const index = tasks.value.findIndex(item => item.taskId === task.taskId);
    if (index < 0) tasks.value.unshift(task);
    else tasks.value[index] = task;
  }

  function remove(taskId: string) {
    tasks.value = tasks.value.filter(task => task.taskId !== taskId);
  }

  function clearTerminal() {
    tasks.value = tasks.value.filter(task => activeStatuses.has(task.status));
  }

  return { tasks, activeCount, upsert, remove, clearTerminal };
});
