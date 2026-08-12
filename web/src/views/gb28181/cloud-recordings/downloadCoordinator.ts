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
  const creatingFileIds = new Set<string>();
  const activeTaskIds = new Set<string>();
  const contentUrls = new Map<string, string>();
  const creatingOperations = new Set<Promise<void>>();
  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let creatingCount = 0;
  let generation = 0;

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

  async function start(request: DownloadRequest, startGeneration: number) {
    const created = await deps.create(request.fileId);
    if (startGeneration !== generation) {
      await deps.cancel(created.task.taskId).catch(() => undefined);
      return;
    }
    if (!isSameOriginContentPath(created.contentUrl, created.task.taskId)) {
      const invalidTask = toItem(created.task, request.fileName);
      items.delete(`queued-${request.fileId}`);
      items.set(invalidTask.taskId, { ...invalidTask, status: "failed", errorCode: "content_url_invalid" });
      publish();
      return;
    }
    const item = toItem(created.task, request.fileName);
    items.delete(`queued-${request.fileId}`);
    items.set(item.taskId, item);
    activeTaskIds.add(item.taskId);
    contentUrls.set(item.taskId, created.contentUrl);
    try {
      deps.startNativeDownload(created.contentUrl, request.fileName);
    } catch {
      items.set(item.taskId, { ...item, errorCode: "browser_start_failed" });
    }
    publish();
    startPolling();
  }

  async function drain() {
    while (activeTaskIds.size + creatingCount < maxConcurrent && pending.length) {
      const next = pending.shift();
      if (!next) return;
      creatingCount += 1;
      creatingFileIds.add(next.fileId);
      const startGeneration = generation;
      const operation = start(next, startGeneration);
      creatingOperations.add(operation);
      try {
        await operation;
      } catch {
        const taskId = `local-${next.fileId}-${Date.now()}`;
        items.delete(`queued-${next.fileId}`);
        items.set(taskId, {
          taskId, fileId: next.fileId, fileName: next.fileName, status: "failed", bytesSent: 0,
          createdAt: new Date().toISOString(), expiresAt: "", errorCode: "create_failed"
        });
        publish();
      } finally {
        creatingOperations.delete(operation);
        creatingFileIds.delete(next.fileId);
        creatingCount -= 1;
      }
    }
  }

  async function enqueue(request: DownloadRequest) {
    if (creatingFileIds.has(request.fileId)
      || [...items.values()].some(item => item.fileId === request.fileId && !terminalStatuses.has(item.status))
      || pending.some(item => item.fileId === request.fileId)) return;
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
      if (items.get(taskId) !== current || !activeTaskIds.has(taskId)) return;
      const next = toItem(response.task, current.fileName);
      items.set(taskId, next);
      if (terminalStatuses.has(next.status)) {
        activeTaskIds.delete(taskId);
        contentUrls.delete(taskId);
        stopPollingWhenIdle();
        await drain();
      }
      publish();
    } catch {
      if (items.get(taskId) !== current || !activeTaskIds.has(taskId)) return;
      items.set(taskId, { ...current, errorCode: "status_unavailable" });
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
    if (terminalStatuses.has(response.task.status)) {
      activeTaskIds.delete(taskId);
      contentUrls.delete(taskId);
      stopPollingWhenIdle();
      await drain();
    }
    publish();
  }

  async function cancelAll() {
    generation += 1;
    pending.length = 0;
    for (const [taskId, item] of items) {
      if (item.status !== "queued") continue;
      items.set(taskId, { ...item, status: "cancelled" });
    }
    const cancellations = [...items.values()]
      .filter(item => activeTaskIds.has(item.taskId))
      .map(item => cancel(item.taskId));
    await Promise.allSettled([...cancellations, ...creatingOperations]);
    activeTaskIds.clear();
    contentUrls.clear();
    items.clear();
    stopPollingWhenIdle();
    publish();
  }

  function clearTerminal() {
    for (const [taskId, item] of items) {
      if (!terminalStatuses.has(item.status)) continue;
      items.delete(taskId);
      contentUrls.delete(taskId);
    }
    publish();
  }

  async function retry(taskId: string) {
    const current = items.get(taskId);
    if (!current) return;
    if (current.status === "ready") {
      const contentUrl = contentUrls.get(taskId);
      if (contentUrl) {
        try {
          deps.startNativeDownload(contentUrl, current.fileName);
          items.set(taskId, { ...current, errorCode: undefined });
        } catch {
          items.set(taskId, { ...current, errorCode: "browser_start_failed" });
        }
        publish();
      }
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

  return { enqueue, refresh, refreshAll, cancel, cancelAll, retry, clearTerminal, snapshot, dispose };
}

function isSameOriginContentPath(contentUrl: string, taskId: string) {
  try {
    const url = new URL(contentUrl, window.location.origin);
    return url.origin === window.location.origin
      && !url.search
      && !url.hash
      && url.pathname === `/api/gb28181/cloud-recordings/downloads/${encodeURIComponent(taskId)}/content`;
  } catch {
    return false;
  }
}
