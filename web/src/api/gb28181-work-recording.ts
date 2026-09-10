import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type WorkRecordingState = "idle" | "starting" | "recording" | "stopping" | "stopped" | "failed" | "unknown";

export interface WorkRecordingSnapshot {
  id: string;
  channelId: number;
  state: WorkRecordingState;
  version: number;
  lastCheckedAt: string | null;
  startedAt: string | null;
  stoppedAt: string | null;
  fileState: string;
  formState: string;
  lastError?: string;
}

export interface StartWorkRecordingRequest {
  channelId: number;
  requestId: string;
}

const path = "gb28181/work-recordings";

export const startWorkRecording = (data: StartWorkRecordingRequest) =>
  http.request<BaseResult<WorkRecordingSnapshot>>("post", baseUrlApi(path), { data });

export const stopWorkRecording = (id: string) =>
  http.request<BaseResult<WorkRecordingSnapshot>>("post", baseUrlApi(`${path}/${encodeURIComponent(id)}/stop`));

export const getWorkRecordingStatus = (channelIds: number[]) =>
  http.request<BaseResult<WorkRecordingSnapshot[]>>("get", baseUrlApi(`${path}/status`), {
    params: { channelIds: channelIds.join(",") }
  });

export interface WorkRecordingListItem extends WorkRecordingSnapshot {
  channelName: string;
}
export interface WorkRecordingList {
  items: WorkRecordingListItem[];
  total: number;
  page: number;
  pageSize: number;
}
export const listWorkRecordings = (page = 1, state?: "active") =>
  http.request<BaseResult<WorkRecordingList>>("get", baseUrlApi(path), {
    params: { page, pageSize: 10, state }
  });

export interface WorkRecordingForm {
  projectName: string;
  major: string;
  stationArea: string;
  mileage: string;
  anchorSectionNo: string;
  startAnchorPillarNo: string;
  endAnchorPillarNo: string;
  workLeader: string;
  workPersonnel: string[];
  tensionWireCarModel: string;
  tensionWireCarNo: string;
  setTension: string;
  straightenerStatus: string;
  straightenerInspector: string;
  wireLayingProcess: string;
  remark: string;
}
export interface WorkRecordingFormDetail {
  jobId: string;
  channelId: number;
  formVersion: number;
  formState: string;
  schemaVersion: number;
  deviceId: string;
  editable: boolean;
  form: WorkRecordingForm;
}
export interface WorkRecordingBatchFormDetail {
  batchId: string;
  formVersion: number;
  formState: string;
  schemaVersion: number;
  deviceId: string;
  editable: boolean;
  form: WorkRecordingForm;
}
export const getWorkRecordingForm = (id: string) =>
  http.request<BaseResult<WorkRecordingFormDetail>>("get", baseUrlApi(`${path}/${encodeURIComponent(id)}/form`));
export const saveWorkRecordingForm = (id: string, formVersion: number, form: WorkRecordingForm) =>
  http.request<BaseResult<WorkRecordingFormDetail>>("put", baseUrlApi(`${path}/${encodeURIComponent(id)}/form`), {
    data: { formVersion, form }
  });
export const getWorkRecordingBatchForm = (id: string) =>
  http.request<BaseResult<WorkRecordingBatchFormDetail>>("get", baseUrlApi(`${path}/batches/${encodeURIComponent(id)}/form`));
export const saveWorkRecordingBatchForm = (id: string, formVersion: number, form: WorkRecordingForm) =>
  http.request<BaseResult<WorkRecordingBatchFormDetail>>("put", baseUrlApi(`${path}/batches/${encodeURIComponent(id)}/form`), {
    data: { formVersion, form }
  });

export interface WorkRecordingBatchCamera {
  channelId: number;
  channelName?: string;
  jobId: string;
  state: WorkRecordingState;
  fileState: string;
  startedAt: string | null;
  stoppedAt: string | null;
  lastError?: string;
  files?: WorkRecordingBatchFile[];
}
export interface WorkRecordingBatchFile {
  id: number;
  channelId: number;
  fileName: string;
  startTime?: string | null;
  timeLen?: number | null;
  fileSize?: number | null;
  state: string;
}
export interface WorkRecordingBatchSnapshot {
  id: string;
  requestId: string;
  state: WorkRecordingState;
  formState: string;
  formVersion: number;
  cameras: WorkRecordingBatchCamera[];
  lastError?: string;
}
export interface WorkRecordingBatchList {
  items: WorkRecordingBatchSnapshot[];
  total: number;
  page: number;
  pageSize: number;
}
export const startWorkRecordingBatch = (data: { channelIds: number[]; requestId: string }) =>
  http.request<BaseResult<WorkRecordingBatchSnapshot>>("post", baseUrlApi(`${path}/batches`), { data });
export const listWorkRecordingBatches = (page = 1) =>
  http.request<BaseResult<WorkRecordingBatchList>>("get", baseUrlApi(`${path}/batches`), { params: { page, pageSize: 10 } });
export const getWorkRecordingBatch = (id: string) =>
  http.request<BaseResult<WorkRecordingBatchSnapshot>>("get", baseUrlApi(`${path}/batches/${encodeURIComponent(id)}`));
export const stopWorkRecordingBatch = (id: string) =>
  http.request<BaseResult<WorkRecordingBatchSnapshot>>("post", baseUrlApi(`${path}/batches/${encodeURIComponent(id)}/stop`));
export const workRecordingBatchDownloadUrl = (id: string) => baseUrlApi(`${path}/batches/${encodeURIComponent(id)}/download`);
export const workRecordingBatchFileUrl = (batchId: string, fileId: number) => baseUrlApi(`${path}/batches/${encodeURIComponent(batchId)}/files/${fileId}`);
