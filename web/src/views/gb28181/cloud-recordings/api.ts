import type { BaseResult } from "@/api/types";
import { baseUrlApi } from "@/api/utils";
import { http } from "@/utils/http";

export type RecordingAvailability = "available" | "node_offline" | "node_missing" | "file_missing" | "access_unavailable";
export type RecordingMetadataState = "complete" | "partial";
export type RecordingAccessMode = "play";

export interface RecordingNode {
  id: string;
  name?: string;
  state?: string;
}

export interface RecordingFile {
  id: string;
  fileKey: string;
  channelId: string;
  channelCode: string;
  channelName: string;
  deviceId: string;
  deviceName: string;
  node: RecordingNode;
  fileName: string;
  startTime: string | null;
  endTime: string | null;
  timeLen: number | null;
  fileSize: number | null;
  source: "hook" | "reconcile" | string;
  metadataState: RecordingMetadataState;
  availability: RecordingAvailability;
  recordDate: string | null;
  discoveredAt: string | null;
  lastSeenAt: string | null;
  missingAt: string | null;
}

export interface RecordingFileQuery {
  page: number;
  pageSize: number;
  start?: string;
  end?: string;
  channelId?: string;
  deviceId?: string;
  nodeId?: string;
  keyword?: string;
  metadataState?: RecordingMetadataState | "";
  availability?: RecordingAvailability | "";
}

export interface RecordingFilePage {
  list: RecordingFile[];
  total: number;
  page: number;
  pageSize: number;
}

export interface RecordingOptions {
  channels: Array<{ id: string; code: string; name: string }>;
  devices: Array<{ id: string; name: string }>;
  nodes: RecordingNode[];
}

export interface RecordingAccess {
  mode: RecordingAccessMode;
  capability: string;
  expiresAt: string;
}

export type RecordingDownloadStatus = "queued" | "ready" | "streaming" | "completed" | "failed" | "cancelled" | "expired";

export interface RecordingDownloadTask {
  taskId: string;
  fileId: string;
  status: RecordingDownloadStatus;
  bytesSent: number;
  totalBytes?: number;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  expiresAt: string;
  errorCode?: string;
  speedBytesPerSecond?: number;
  etaSeconds?: number;
}

export interface RecordingDownloadCreation {
  task: RecordingDownloadTask;
  contentUrl: string;
}

export interface RecordingDeleteResult {
  id: string;
  deleted: boolean;
  errorCode?: string;
}

export interface RecordingBatchDeleteResult {
  deletedCount: number;
  failedCount: number;
  results: RecordingDeleteResult[];
}

export interface ActiveRecording {
  id: string;
  channelId: string;
  channelCode: string;
  channelName: string;
  deviceId: string;
  node: RecordingNode;
  state: string;
  startedAt: string | null;
  updatedAt: string;
}

export interface StopActiveRecordingResult {
  id: string;
  channelId: string;
  stopped: boolean;
}

export interface RecordingReconciliation {
  node: RecordingNode;
  status: "queued" | "running" | "succeeded" | "partial" | "failed" | string;
  triggerSource: string;
  effectiveStart: string | null;
  effectiveEnd: string | null;
  startedAt: string | null;
  finishedAt: string | null;
  candidateCount: number;
  successCount: number;
  failureCount: number;
  discoveredCount: number;
  insertedCount: number;
  updatedCount: number;
  missingCount: number;
  unattributedCount: number;
  updatedAt: string;
}

export interface ReconciliationRequest {
  nodeIds?: number[];
  start?: string;
  end?: string;
}

function suppliedQuery<T extends object>(query: T) {
  return Object.fromEntries(Object.entries(query).filter(([, value]) => value !== undefined && value !== null && value !== ""));
}

export function listRecordingFiles(query: RecordingFileQuery, signal?: AbortSignal) {
  return http.request<BaseResult<RecordingFilePage>>("get", baseUrlApi("gb28181/cloud-recordings/files"), {
    params: suppliedQuery(query),
    ...(signal ? { signal } : {})
  });
}

export function listRecordingOptions(query: Pick<RecordingFileQuery, "start" | "end">) {
  return http.request<BaseResult<RecordingOptions>>("get", baseUrlApi("gb28181/cloud-recordings/files/options"), {
    params: suppliedQuery(query)
  });
}

export function getRecordingDetail(id: string) {
  return http.request<BaseResult<RecordingFile>>(
    "get",
    baseUrlApi(`gb28181/cloud-recordings/files/${encodeURIComponent(id)}`),
    undefined,
    { showErrorMessage: false }
  );
}

export function deleteRecordingFile(id: string) {
  return http.request<BaseResult<RecordingDeleteResult>>(
    "delete",
    baseUrlApi(`gb28181/cloud-recordings/files/${encodeURIComponent(id)}`),
    undefined,
    { showErrorMessage: false }
  );
}

export function batchDeleteRecordingFiles(ids: string[]) {
  return http.request<BaseResult<RecordingBatchDeleteResult>>(
    "post",
    baseUrlApi("gb28181/cloud-recordings/files/batch-delete"),
    { data: { ids } },
    { showErrorMessage: false }
  );
}

export function issueRecordingAccess(id: string, mode: RecordingAccessMode) {
  return http.request<BaseResult<RecordingAccess>>(
    "post",
    baseUrlApi(`gb28181/cloud-recordings/files/${encodeURIComponent(id)}/access`),
    { data: { mode } },
    { showErrorMessage: false }
  );
}

export function createRecordingDownload(id: string) {
  return http.request<BaseResult<RecordingDownloadCreation>>(
    "post",
    baseUrlApi(`gb28181/cloud-recordings/files/${encodeURIComponent(id)}/downloads`),
    undefined,
    { showErrorMessage: false }
  );
}

export function getRecordingDownload(taskId: string) {
  return http.request<BaseResult<RecordingDownloadTask>>(
    "get",
    baseUrlApi(`gb28181/cloud-recordings/downloads/${encodeURIComponent(taskId)}`),
    undefined,
    { showErrorMessage: false }
  );
}

export function cancelRecordingDownload(taskId: string) {
  return http.request<BaseResult<RecordingDownloadTask>>(
    "delete",
    baseUrlApi(`gb28181/cloud-recordings/downloads/${encodeURIComponent(taskId)}`),
    undefined,
    { showErrorMessage: false }
  );
}

export function listActiveRecordings() {
  return http.request<BaseResult<{ list: ActiveRecording[] }>>(
    "get",
    baseUrlApi("gb28181/cloud-recordings/active"),
    undefined,
    { showErrorMessage: false }
  );
}

export function stopActiveRecording(id: string) {
  return http.request<BaseResult<StopActiveRecordingResult>>(
    "post",
    baseUrlApi(`gb28181/cloud-recordings/active/${encodeURIComponent(id)}/stop`),
    undefined,
    { showErrorMessage: false }
  );
}

export function listReconciliations() {
  return http.request<BaseResult<{ list: RecordingReconciliation[] }>>(
    "get",
    baseUrlApi("gb28181/cloud-recordings/reconciliations"),
    undefined,
    { showErrorMessage: false }
  );
}

export function triggerReconciliation(data: ReconciliationRequest) {
  return http.request<BaseResult<{ acceptedNodeIds: number[] }>>("post", baseUrlApi("gb28181/cloud-recordings/reconciliations"), { data });
}

export function contentURL(id: string, capability: string) {
  return `${baseUrlApi(`gb28181/cloud-recordings/content/${encodeURIComponent(id)}`)}?cap=${encodeURIComponent(capability)}`;
}
