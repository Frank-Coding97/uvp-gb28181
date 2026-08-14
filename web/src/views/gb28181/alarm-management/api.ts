import type { BaseResult } from "@/api/types";
import { baseUrlApi } from "@/api/utils";
import { http } from "@/utils/http";

export interface AlarmEnumValue {
  value: number | null;
  label: string;
}

export interface AlarmEntitySummary {
  id: number;
  code: string;
  name: string;
  alias: string;
}

export interface AlarmListItem {
  id: string;
  receivedAt: string;
  alarmTime: string | null;
  device: AlarmEntitySummary;
  channel: AlarmEntitySummary | null;
  sourceCode: string;
  priority: AlarmEnumValue;
  method: AlarmEnumValue;
  alarmType: AlarmEnumValue;
  description: string;
}

export interface AlarmDetail extends AlarmListItem {
  alarmTypeParam: string;
  longitude: number | null;
  latitude: number | null;
  rawDigest: string;
  rawSummary: string;
  createdAt: string;
  updatedAt: string;
}

export interface AlarmQuery {
  page: number;
  pageSize: number;
  deviceId?: number;
  sourceCode?: string;
  alarmFrom?: string;
  alarmTo?: string;
  priority?: number;
  method?: number;
  alarmType?: number;
  keyword?: string;
}

export interface AlarmPage {
  list: AlarmListItem[];
  total: number;
  page: number;
  pageSize: number;
}

export interface DeleteAlarmResult {
  deletedIds: string[];
  deletedCount: number;
}

function suppliedQuery(query: AlarmQuery): Record<string, string | number> {
  return Object.fromEntries(
    Object.entries(query).filter(([, value]) => value !== undefined && value !== null && value !== "")
  ) as Record<string, string | number>;
}

export function listAlarms(query: AlarmQuery) {
  return http.request<BaseResult<AlarmPage>>("get", baseUrlApi("gb28181/alarms"), {
    params: suppliedQuery(query)
  });
}

export function getAlarmDetail(id: string) {
  return http.request<BaseResult<AlarmDetail>>("get", baseUrlApi(`gb28181/alarms/${id}`));
}

export function deleteAlarm(id: string) {
  return http.request<BaseResult<DeleteAlarmResult>>("delete", baseUrlApi(`gb28181/alarms/${id}`));
}

export function batchDeleteAlarms(ids: string[]) {
  return http.request<BaseResult<DeleteAlarmResult>>("post", baseUrlApi("gb28181/alarms/batch-delete"), {
    data: { ids }
  });
}

export function clearAllAlarms() {
  return http.request<BaseResult<{ deletedCount: number }>>("post", baseUrlApi("gb28181/alarms/clear-all"));
}
