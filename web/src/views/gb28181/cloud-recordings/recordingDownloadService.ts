import pinia from "@/store";
import { useRecordingDownloadStore, type RecordingDownloadItem } from "@/store/modules/recording-downloads";
import { cancelRecordingDownload, createRecordingDownload, getRecordingDownload } from "./api";
import { createDownloadCoordinator } from "./downloadCoordinator";
import { registerUserLogoutCleanup } from "@/store/modules/user";

function startNativeDownload(contentUrl: string, fileName: string) {
  const anchor = document.createElement("a");
  anchor.href = contentUrl;
  anchor.download = fileName;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

const store = useRecordingDownloadStore(pinia);

export const recordingDownloadCoordinator = createDownloadCoordinator({
  create: async fileId => (await createRecordingDownload(fileId)).data,
  get: async taskId => ({ task: (await getRecordingDownload(taskId)).data }),
  cancel: async taskId => ({ task: (await cancelRecordingDownload(taskId)).data }),
  startNativeDownload,
  onChange: (items: RecordingDownloadItem[]) => {
    const taskIds = new Set(items.map(item => item.taskId));
    (store.tasks as RecordingDownloadItem[])
      .filter((item: RecordingDownloadItem) => !taskIds.has(item.taskId))
      .forEach((item: RecordingDownloadItem) => store.remove(item.taskId));
    items.forEach(item => store.upsert(item));
  }
});

registerUserLogoutCleanup(() => recordingDownloadCoordinator.cancelAll());
