import pinia from "@/store";
import { useRecordingDownloadStore } from "@/store/modules/recording-downloads";
import { cancelRecordingDownload, createRecordingDownload, getRecordingDownload } from "./api";
import { createDownloadCoordinator } from "./downloadCoordinator";

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
  onChange: items => items.forEach(item => store.upsert(item))
});
