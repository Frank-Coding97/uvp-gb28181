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
