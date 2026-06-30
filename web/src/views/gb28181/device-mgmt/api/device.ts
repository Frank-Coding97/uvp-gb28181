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

export function listDevices(params: ListDevicesParams = {}): Promise<BaseResult<PageResult<DeviceVO>>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/devices"),
    method: "get",
    params,
  });
}

export function getDevice(id: number): Promise<BaseResult<DeviceVO>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/device/${id}`),
    method: "get",
  });
}

// ============================================================
// 接口 - 通道
// ============================================================

export function listChannels(params: ListChannelsParams = {}): Promise<BaseResult<PageResult<ChannelVO>>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/channels"),
    method: "get",
    params,
  });
}

export function getChannel(id: number): Promise<BaseResult<ChannelVO>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/channel/${id}`),
    method: "get",
  });
}

export function getChannelMounts(id: number): Promise<BaseResult<{ list: ChannelMountVO[]; total: number }>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/channel/${id}/mounts`),
    method: "get",
  });
}

export function getChannelTimeline(
  id: number,
  range: "24h" = "24h"
): Promise<BaseResult<{ slots: TimelineSlot[]; range: string; channelId: number; phase1Simplified: boolean }>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/channel/${id}/timeline`),
    method: "get",
    params: { range },
  });
}

// ============================================================
// 接口 - 地图
// ============================================================

export function getMapMarkers(bbox: Partial<MarkerBBox> = {}): Promise<BaseResult<{ list: MarkerVO[]; total: number }>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/map/markers"),
    method: "get",
    params: bbox,
  });
}

export function getMapClusters(
  params: Partial<MarkerBBox> & { zoom: number }
): Promise<BaseResult<{ clusters: ClusterVO[]; zoom: number; gridSize: number }>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/map/clusters"),
    method: "get",
    params,
  });
}

export function getMapNoCoordCount(): Promise<BaseResult<{ count: number }>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/map/no-coord-count"),
    method: "get",
  });
}

// ============================================================
// 接口 - 异常治理
// ============================================================

export function listAnomalies(
  params: { resolved?: 0 | 1; page?: number; pageSize?: number } = {}
): Promise<BaseResult<PageResult<AnomalyVO>>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/anomaly"),
    method: "get",
    params,
  });
}

export function resolveAnomaly(id: number, body: ResolveBody): Promise<BaseResult<{ id: number; ok: boolean }>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/anomaly/${id}/resolve`),
    method: "post",
    data: body,
  });
}

export interface BatchResolveBody {
  ids: number[];
  action: "change-type" | "change-mount" | "mark-resolved";
  targetType?: NodeTypeName;
  targetParentId?: number;
}

export function batchResolveAnomaly(
  body: BatchResolveBody
): Promise<BaseResult<{ succeeded: number[]; failed: Array<{ id: number; error: string }> }>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/anomaly/batch-resolve"),
    method: "post",
    data: body,
  });
}
