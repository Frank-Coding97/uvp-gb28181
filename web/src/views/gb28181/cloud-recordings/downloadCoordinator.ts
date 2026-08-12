import type { RecordingDownloadCreation, RecordingDownloadTask } from "./api";
import type { RecordingDownloadItem } from "@/store/modules/recording-downloads";

export interface DownloadRequest {
  fileId: string;
  fileName: string;
}

interface CoordinatorDependencies {
  create: (fileId: string) => Promise<RecordingDownloadCreation>;
  get: (taskId: string) => Promise<{ task: RecordingDownloadTask }>;
  cancel: (taskId: string) => Promise<{ task: RecordingDownloadTask }>;
  startNativeDownload: (contentUrl: string, fileName: string) => void;
  onChange?: (items: RecordingDownloadItem[]) => void;
  pollIntervalMs?: number;
}

const terminalStatuses = new Set(["completed", "failed", "cancelled", "expired"]);

export function createDownloadCoordinator(deps: CoordinatorDependencies) {
  const maxConcurrent = 2;
  const pollIntervalMs = deps.pollIntervalMs ?? 1500;
  const items = new Map<string, RecordingDownloadItem>();
  const pending: DownloadRequest[] = [];
  const activeTaskIds = new Set<string>();
  const contentUrls = new Map<string, string>();
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  const publish = () => deps.onChange?.(snapshot());
  const toItem = (task: RecordingDownloadTask, fileName: string): RecordingDownloadItem => ({ ...task, fileName });

  function snapshot() {
    return [...items.values()];
  }

  function startPolling() {
    if (!pollTimer && activeTaskIds.size) pollTimer = setInterval(() => void refreshAll(), pollIntervalMs);
  }

  function stopPollingWhenIdle() {
    if (!activeTaskIds.size && pollTimer) {
      clearInterval(pollTimer);
      pollTimer = undefined;
    }
  }

  async function start(request: DownloadRequest) {
    const created = await deps.create(request.fileId);
    if (!isSameOriginContentPath(created.contentUrl)) {
      const invalidTask = toItem(created.task, request.fileName);
      items.set(invalidTask.taskId, { ...invalidTask, status: "failed", errorCode: "content_url_invalid" });
      publish();
      return;
    }
    const item = toItem(created.task, request.fileName);
    items.delete(`queued-${request.fileId}`);
    items.set(item.taskId, item);
    activeTaskIds.add(item.taskId);
    contentUrls.set(item.taskId, created.contentUrl);
    deps.startNativeDownload(created.contentUrl, request.fileName);
    publish();
    startPolling();
  }

  async function drain() {
    while (activeTaskIds.size < maxConcurrent && pending.length) {
      const next = pending.shift();
      if (!next) return;
      try {
        await start(next);
      } catch {
        const taskId = `local-${next.fileId}-${Date.now()}`;
        items.set(taskId, {
          taskId, fileId: next.fileId, fileName: next.fileName, status: "failed", bytesSent: 0,
          createdAt: new Date().toISOString(), expiresAt: "", errorCode: "create_failed"
        });
        publish();
      }
    }
  }

  async function enqueue(request: DownloadRequest) {
    if ([...items.values()].some(item => item.fileId === request.fileId && !terminalStatuses.has(item.status)) || pending.some(item => item.fileId === request.fileId)) return;
    pending.push(request);
    await drain();
    if (pending.some(item => item.fileId === request.fileId)) {
      const queuedTaskId = `queued-${request.fileId}`;
      if (!items.has(queuedTaskId)) {
        items.set(queuedTaskId, {
          taskId: queuedTaskId, fileId: request.fileId, fileName: request.fileName, status: "queued", bytesSent: 0,
          createdAt: new Date().toISOString(), expiresAt: ""
        });
        publish();
      }
    }
  }

  async function refresh(taskId: string) {
    const current = items.get(taskId);
    if (!current || !activeTaskIds.has(taskId)) return;
    try {
      const response = await deps.get(taskId);
      const next = toItem(response.task, current.fileName);
      items.set(taskId, next);
      if (terminalStatuses.has(next.status)) {
        activeTaskIds.delete(taskId);
        stopPollingWhenIdle();
        await drain();
      }
      publish();
    } catch {
      items.set(taskId, { ...current, status: "failed", errorCode: "status_unavailable" });
      activeTaskIds.delete(taskId);
      stopPollingWhenIdle();
      await drain();
      publish();
    }
  }

  async function refreshAll() {
    await Promise.all([...activeTaskIds].map(taskId => refresh(taskId)));
  }

  async function cancel(taskId: string) {
    const current = items.get(taskId);
    if (!current) return;
    if (current.status === "queued") {
      const index = pending.findIndex(item => item.fileId === current.fileId);
      if (index >= 0) pending.splice(index, 1);
      items.set(taskId, { ...current, status: "cancelled" });
      publish();
      return;
    }
    const response = await deps.cancel(taskId);
    items.set(taskId, toItem(response.task, current.fileName));
    activeTaskIds.delete(taskId);
    stopPollingWhenIdle();
    await drain();
    publish();
  }

  async function retry(taskId: string) {
    const current = items.get(taskId);
    if (!current) return;
    if (current.status === "ready") {
      const contentUrl = contentUrls.get(taskId);
      if (contentUrl) deps.startNativeDownload(contentUrl, current.fileName);
      return;
    }
    if (!terminalStatuses.has(current.status)) return;
    await enqueue({ fileId: current.fileId, fileName: current.fileName });
  }

  function dispose() {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = undefined;
    contentUrls.clear();
  }

  return { enqueue, refresh, refreshAll, cancel, retry, snapshot, dispose };
}

function isSameOriginContentPath(contentUrl: string) {
  if (!contentUrl.startsWith("/")) return false;
  return /^\/api\/gb28181\/cloud-recordings\/downloads\/[^/]+\/content$/.test(contentUrl);
}
