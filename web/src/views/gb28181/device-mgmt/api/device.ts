/**
 * 设备管理页 - 设备/通道 REST API client(B2 devicemgmt + B3 map + B4 anomaly)
 *
 * 后端路由前缀:/api/gb28181/device-mgmt/
 */

import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

// ============================================================
// 类型
// ============================================================

export interface DeviceVO {
  id: number;
  tenantId: number;
  deviceId: string;
  name: string;
  manufacturer: string;
  model: string;
  firmware: string;
  ip: string;
  port: number;
  status: number;
  online: boolean;
  channelCount: number;
  channelOnlineCount: number;
  onlineRate: number;
  subscribeCapability: "unknown" | "subscribed" | "fallback";
  subscribeLastTest: string | null;
  subscribeExpiresAt: string | null;
  registerTime: string | null;
  keepaliveTime: string | null;
}

export interface ChannelVO {
  id: number;
  tenantId: number;
  deviceId: string;
  channelId: string;
  name: string;
  manufacturer: string;
  model: string;
  owner: string;
  civilCode: string;
  parentId: string;
  ptzType: number;
  longitude: number;
  latitude: number;
  status: number;
  streamId: string;
  capabilities: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ChannelMountVO {
  id: number;
  parentNodeId: number;
  parentName: string;
  parentPath: string;
  displayName: string;
  isPrimary: boolean;
  mountSource: "catalog" | "manual";
}

export interface TimelineSlot {
  start: string;
  end: string;
  status: "online" | "offline" | "warning";
}

export interface MarkerVO {
  id: number;
  channelId: string;
  name: string;
  latitude: number;
  longitude: number;
  status: number;
}

export interface ClusterVO {
  centerLat: number;
  centerLng: number;
  count: number;
  onlineCount: number;
  onlineRate: number;
}

export interface AnomalyVO {
  id: number;
  tenantId: number;
  catalogNodeId: number;
  rawCode: string;
  guessedType: string;
  fallbackType: "virtual_org" | "channel" | "device";
  sourceDeviceId: number | null;
  reason: string;
  resolved: boolean;
  resolvedBy: number | null;
  resolvedAt: string | null;
  resolvedAction: string;
  createdAt: string;
  nodeName: string;
  nodePath: string;
}

export interface PageResult<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

export type DeviceStatus = "online" | "offline";

export interface ListDevicesParams {
  page?: number;
  pageSize?: number;
  status?: DeviceStatus;
  vendor?: string;
  q?: string;
  /** "name:asc" / "heartbeat:desc" / "id:desc" */
  sort?: string;
}

export interface ListChannelsParams {
  page?: number;
  pageSize?: number;
  nodeId?: number;
  status?: DeviceStatus;
  ptz?: 0 | 1;
  q?: string;
}

export interface MarkerBBox {
  minLat: number;
  maxLat: number;
  minLng: number;
  maxLng: number;
  limit?: number;
}

export interface ResolveBody {
  action: "change-type" | "change-mount" | "mark-resolved";
  targetType?: NodeTypeName;
  targetParentId?: number;
  note?: string;
}

export type NodeTypeName = "civil_code" | "biz_group" | "virtual_org" | "device" | "channel";

// ============================================================
// 接口 - 设备
// ============================================================

export const listDevices = (params: ListDevicesParams = {}) =>
  http.request<BaseResult<PageResult<DeviceVO>>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/devices"),
    { params }
  );

export const getDevice = (id: number) =>
  http.request<BaseResult<DeviceVO>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}`));

// ============================================================
// 接口 - 通道
// ============================================================

export const listChannels = (params: ListChannelsParams = {}) =>
  http.request<BaseResult<PageResult<ChannelVO>>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/channels"),
    { params }
  );

export const getChannel = (id: number) =>
  http.request<BaseResult<ChannelVO>>("get", baseUrlApi(`gb28181/device-mgmt/channel/${id}`));

export const getChannelMounts = (id: number) =>
  http.request<BaseResult<{ list: ChannelMountVO[]; total: number }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${id}/mounts`)
  );

export const getChannelTimeline = (id: number, range: "24h" = "24h") =>
  http.request<
    BaseResult<{ slots: TimelineSlot[]; range: string; channelId: number; phase1Simplified: boolean }>
  >("get", baseUrlApi(`gb28181/device-mgmt/channel/${id}/timeline`), { params: { range } });

// ============================================================
// 接口 - 地图
// ============================================================

export const getMapMarkers = (bbox: Partial<MarkerBBox> = {}) =>
  http.request<BaseResult<{ list: MarkerVO[]; total: number }>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/map/markers"),
    { params: bbox }
  );

export const getMapClusters = (params: Partial<MarkerBBox> & { zoom: number }) =>
  http.request<BaseResult<{ clusters: ClusterVO[]; zoom: number; gridSize: number }>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/map/clusters"),
    { params }
  );

export const getMapNoCoordCount = () =>
  http.request<BaseResult<{ count: number }>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/map/no-coord-count")
  );

// ============================================================
// 接口 - 异常治理
// ============================================================

export const listAnomalies = (params: { resolved?: 0 | 1; page?: number; pageSize?: number } = {}) =>
  http.request<BaseResult<PageResult<AnomalyVO>>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/anomaly"),
    { params }
  );

export const resolveAnomaly = (id: number, body: ResolveBody) =>
  http.request<BaseResult<{ id: number; ok: boolean }>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/anomaly/${id}/resolve`),
    { data: body }
  );

export interface BatchResolveBody {
  ids: number[];
  action: "change-type" | "change-mount" | "mark-resolved";
  targetType?: NodeTypeName;
  targetParentId?: number;
}

export const batchResolveAnomaly = (body: BatchResolveBody) =>
  http.request<BaseResult<{ succeeded: number[]; failed: Array<{ id: number; error: string }> }>>(
    "post",
    baseUrlApi("gb28181/device-mgmt/anomaly/batch-resolve"),
    { data: body }
  );
