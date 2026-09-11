import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export interface RecordingPlanPeriod {
  weekday: number;
  startSlot: number;
  endSlot: number;
}

export interface RecordingPlanSummary {
  id: number;
  name: string;
  description: string;
  enabled: boolean;
  version: number;
  channelCount: number;
	periods: RecordingPlanPeriod[];
  updatedAt: string;
}

export interface RecordingPlanDetail extends RecordingPlanSummary {
  ownerDeptId: number;
  periods: RecordingPlanPeriod[];
  createdAt: string;
}

export interface RecordingPlanInput {
  name: string;
  description: string;
  enabled: boolean;
  periods: RecordingPlanPeriod[];
}

export interface AssignmentOption {
  id: number;
  name: string;
  code: string;
  deviceCode?: string;
  online: boolean;
  bound: boolean;
}

export interface PageData<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface PlanChannelStatus {
  channelId: number;
  channelCode: string;
  channelName: string;
  deviceCode: string;
  deviceName: string;
  online: boolean;
  recordingMode: "off" | "continuous" | "scheduled";
  desiredState: string;
  actualState: string;
  reasonCode: string;
  reasonMessage: string;
  nextTransitionAt: string | null;
  nextRetryAt: string | null;
  attemptCount: number;
  lastMediaAt: string | null;
  lastSuccessAt: string | null;
}

export interface PlanChannelPage extends PageData<PlanChannelStatus> {
  statusCounts: Record<string, number>;
}

export interface AssignmentResult {
  items: Array<{ id: number; status: "assigned" | "conflict" | "forbidden" | "not_found" }>;
  assignedCount: number;
}

const path = "gb28181/recording-plans";

export const listRecordingPlans = (params: { keyword?: string; status?: string; page: number; pageSize: number }) =>
  http.request<BaseResult<PageData<RecordingPlanSummary>>>("get", baseUrlApi(path), { params });
export const getRecordingPlan = (id: number) =>
  http.request<BaseResult<RecordingPlanDetail>>("get", baseUrlApi(`${path}/${id}`));
export const createRecordingPlan = (data: RecordingPlanInput) =>
  http.request<BaseResult<RecordingPlanDetail>>("post", baseUrlApi(path), { data });
export const updateRecordingPlan = (id: number, data: RecordingPlanInput) =>
  http.request<BaseResult<RecordingPlanDetail>>("put", baseUrlApi(`${path}/${id}`), { data });
export const setRecordingPlanEnabled = (id: number, enabled: boolean) =>
  http.request<BaseResult<RecordingPlanDetail>>("patch", baseUrlApi(`${path}/${id}/status`), { data: { enabled } });
export const deleteRecordingPlan = (id: number) =>
  http.request<BaseResult<null>>("delete", baseUrlApi(`${path}/${id}`));
export const listRecordingPlanDevices = (id: number, params: { keyword?: string; online?: string; page: number; pageSize: number }) =>
  http.request<BaseResult<PageData<AssignmentOption>>>("get", baseUrlApi(`${path}/${id}/assignment-options/devices`), { params });
export const listRecordingPlanChannels = (id: number, params: { keyword?: string; online?: string; page: number; pageSize: number }) =>
  http.request<BaseResult<PageData<AssignmentOption>>>("get", baseUrlApi(`${path}/${id}/assignment-options/channels`), { params });
export const assignRecordingPlan = (id: number, data: { type: "device" | "channel"; ids: number[] }) =>
  http.request<BaseResult<AssignmentResult>>("post", baseUrlApi(`${path}/${id}/assignments`), { data });
export const listRecordingPlanExecutionChannels = (id: number, params: { keyword?: string; online?: string; actualState?: string; page: number; pageSize: number }) =>
  http.request<BaseResult<PlanChannelPage>>("get", baseUrlApi(`${path}/${id}/channels`), { params });
export const diagnoseRecordingPlanChannel = (channelId: number) =>
  http.request<BaseResult<Record<string, unknown>>>("get", baseUrlApi(`${path}/channels/${channelId}/diagnosis`));
export const getRecordingPlanChannelTimeline = (channelId: number, params: { page: number; pageSize: number }) =>
  http.request<BaseResult<Record<string, unknown>>>("get", baseUrlApi(`${path}/channels/${channelId}/timeline`), { params });
