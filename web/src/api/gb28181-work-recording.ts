import { http } from "@/utils/http";
import { getAccessToken } from "@/utils/auth";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

/**
 * 作业单是录制的唯一入口：一张作业单同时录制 1~N 路正在播放的画面。
 * 旧的「单通道作业」与「批次台账」两套并行链路已删除，接口统一收敛到这里。
 */

export type WorkRecordingState = "idle" | "starting" | "recording" | "stopping" | "stopped" | "failed" | "unknown";

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

/** 单个已归档的录像分片。 */
export interface WorkOrderSlice {
  id: number;
  channelId: number;
  fileName: string;
  startTime?: string | null;
  timeLen?: number | null;
  fileSize?: number | null;
  state: string;
}

/** 作业单下的每一路通道录像。 */
export interface WorkOrderCamera {
  channelId: number;
  channelName?: string;
  /** 通道国标编码（20 位）。 */
  channelCode?: string;
  /** 所属设备国标编码。 */
  deviceId?: string;
  deviceName?: string;
  jobId: string;
  state: WorkRecordingState;
  fileState: string;
  startedAt: string | null;
  stoppedAt: string | null;
  lastError?: string;
  files?: WorkOrderSlice[];
}

export interface WorkOrderSnapshot {
  id: string;
  requestId: string;
  state: WorkRecordingState;
  formState: string;
  formVersion: number;
  projectName?: string;
  cameras: WorkOrderCamera[];
  lastError?: string;
}

export interface WorkOrderList {
  items: WorkOrderSnapshot[];
  total: number;
  page: number;
  pageSize: number;
}

export interface WorkOrderFormRecord {
  batchId: string;
  formVersion: number;
  formState: string;
  schemaVersion: number;
  deviceId: string;
  editable: boolean;
  form: WorkRecordingForm;
}

export interface WorkOrderDetail {
  snapshot: WorkOrderSnapshot;
  form?: WorkOrderFormRecord;
}

export interface WorkOrderListQuery {
  page?: number;
  pageSize?: number;
  state?: string;
  keyword?: string;
  channelId?: number;
  startTime?: string;
  endTime?: string;
}

const orderPath = "gb28181/work-orders";

/** 先填作业单、再开始录制：表单校验通过后才会占用录制资源。 */
export const createWorkOrder = (data: { requestId: string; channelIds: number[]; form: WorkRecordingForm }) =>
  http.request<BaseResult<WorkOrderSnapshot>>("post", baseUrlApi(orderPath), { data });

export const listWorkOrders = (query: WorkOrderListQuery = {}) =>
  http.request<BaseResult<WorkOrderList>>("get", baseUrlApi(orderPath), { params: query });

export const getWorkOrder = (id: string) =>
  http.request<BaseResult<WorkOrderDetail>>("get", baseUrlApi(`${orderPath}/${encodeURIComponent(id)}`));

export const stopWorkOrder = (id: string) =>
  http.request<BaseResult<WorkOrderSnapshot>>("post", baseUrlApi(`${orderPath}/${encodeURIComponent(id)}/stop`));

/**
 * 删除结果：进行中的作业单会被跳过而不是整批失败，所以两个列表都要给用户看。
 */
export interface WorkOrderDeleteResult {
  deleted: number;
  skipped?: string[];
  missing?: string[];
}

/** 单条删除。进行中的作业单会被服务端拒绝（409）。 */
export const deleteWorkOrder = (id: string) =>
  http.request<BaseResult<WorkOrderDeleteResult>>("delete", baseUrlApi(`${orderPath}/${encodeURIComponent(id)}`));

/** 勾选批量删除。返回实际删除数量与被跳过的进行中作业单。 */
export const batchDeleteWorkOrders = (ids: string[]) =>
  http.request<BaseResult<WorkOrderDeleteResult>>("post", baseUrlApi(`${orderPath}/batch-delete`), { data: { ids } });

/** 多屏页据此恢复「结束录像」按钮状态，避免刷新后误判为未录制。 */
export const getActiveWorkOrder = () =>
  http.request<BaseResult<WorkOrderSnapshot | null>>("get", baseUrlApi(`${orderPath}/active`));

/**
 * 浏览器直达的下载/播放链接由 `window.open` 或 `<a href>` 发起，无法携带
 * `Authorization` 头，只能走 `?token=` 兜底鉴权（后端 `common.GetAccessToken`
 * 支持该通道），与 `sipDashboardStreamUrl` 保持同一约定。
 */
const browserTokenQuery = () => {
  const accessToken = getAccessToken()?.accessToken;
  return accessToken ? `?token=${encodeURIComponent(accessToken)}` : "";
};

export const workOrderDownloadUrl = (id: string) =>
  `${baseUrlApi(`${orderPath}/${encodeURIComponent(id)}/download`)}${browserTokenQuery()}`;

export const workOrderFileUrl = (id: string, fileId: number) =>
  `${baseUrlApi(`${orderPath}/${encodeURIComponent(id)}/files/${fileId}`)}${browserTokenQuery()}`;

/** 表单历史值的一条记录。值用于 a-auto-complete 的下拉项。 */
export interface WorkOrderFormHistoryEntry {
  value: string;
  useCount: number;
  lastUsedAt: string;
}

/**
 * 拉取某字段的历史值。field 必须在 4 个白名单内（projectName / stationArea / workLeader / workPersonnel），
 * 后端会再次校验，越权读取时返回 400。
 */
export const listWorkOrderFormHistory = (field: string, limit = 50) =>
  http.request<BaseResult<{ items: WorkOrderFormHistoryEntry[] }>>(
    "get",
    baseUrlApi(`${orderPath}/form-history?field=${encodeURIComponent(field)}&limit=${limit}`)
  );
