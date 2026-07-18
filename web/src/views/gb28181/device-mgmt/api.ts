import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

export type OnlineStatus = "online" | "offline";
export type AssetKind = "channel" | "device";

export interface PageResult<T> {
    list: T[];
    total: number;
    page: number;
    pageSize: number;
}

export interface CatalogNode {
    id: number;
    nodeType: "civil_code" | "biz_group" | "virtual_org" | "device" | "channel" | string;
    parentId?: number | null;
    path: string;
    depth: number;
    name: string;
    code: string;
    civilCode: string;
    deviceId?: number | null;
    channelId?: number | null;
    source: string;
    sortOrder: number;
    anomaly: boolean;
    anomalyReason: string;
    rawCode: string;
    mountCount?: number;
    childCount?: number;
    anomalyCount?: number;
    createdAt: string;
    updatedAt: string;
}

export type DirectoryDimension = "native" | "biz_group" | "civil_code";

// 三维目录接口返回的统一节点(与后端 directory.Node 对齐)
// 注意:id 是 string —— native/biz_group 为数字串,civil_code 为合成串(province:34 / city:3402 / district:340200 / unassigned)
export interface DirectoryNode {
    id: string;
    name: string;
    nodeType: string;
    parentId?: string | null;
    channelId?: string | null;
    deviceId?: string | null;
    civilCode?: string;
    mountCount?: number;
    channelCount?: number;
    hasChildren: boolean;
    isLeaf: boolean;
    // 以下字段用于 drawer 节点详情展示(native 维度回填,其他维度可能为空)
    anomaly?: boolean;
    childCount?: number;
    path?: string;
    code?: string;
    source?: string;
}

export interface DeviceVO {
    id: number;
    deviceId: string;
    name: string;
    transport: string;
    manufacturer: string;
    model: string;
    firmware: string;
    ip: string;
    port: number;
    status: number;
    online: boolean;
    registerTime?: string | null;
    registerExpireAt?: string | null;
    keepaliveTime?: string | null;
    keepaliveInterval?: number;
    offlineAt?: string | null;
    subscribeCapability?: string;
    channelCount: number;
    channelOnlineCount: number;
    onlineRate: number;
    createdAt: string;
    updatedAt: string;
}

export interface ChannelVO {
    id: number;
    channelId: string;
    deviceId: string;
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
    streamTransport: string; // 流传输模式: UDP / TCP-Active / TCP-Passive
    createdAt: string;
    updatedAt: string;
}

export interface ChannelMount {
    id: number;
    parentNodeId: number;
    parentName: string;
    parentPath: string;
    displayName: string;
    isPrimary: boolean;
    mountSource: string;
}

export interface TimelineSlot {
    start: string;
    end: string;
    status: OnlineStatus;
}

export interface MapMarker {
    id: number;
    channelId: string;
    name: string;
    latitude: number;
    longitude: number;
    status: number;
}

export interface MapCluster {
    centerLat: number;
    centerLng: number;
    count: number;
    onlineCount: number;
    onlineRate: number;
}

export interface AnomalyRecord {
    id: number;
    catalogNodeId: number;
    rawCode: string;
    guessedType: string;
    fallbackType: string;
    sourceDeviceId?: number | null;
    reason: string;
    resolved: boolean;
    resolvedAt?: string | null;
    resolvedAction: string;
    createdAt: string;
    nodeName: string;
    nodePath: string;
}

export interface DeviceQuery {
    q?: string;
    nodeId?: number;
    status?: OnlineStatus;
    vendor?: string;
    page?: number;
    pageSize?: number;
    sort?: string;
}

export interface ChannelQuery {
    q?: string;
    nodeId?: number;
    civilCode?: string;
    status?: OnlineStatus;
    ptz?: "1";
    page?: number;
    pageSize?: number;
}

export const listCatalogRoots = () =>
    http.request<BaseResult<{ list: CatalogNode[]; total: number }>>(
        "get",
        baseUrlApi("gb28181/device-mgmt/catalog/tree")
    );

export const listCatalogChildren = (id: number) =>
    http.request<BaseResult<{ list: CatalogNode[]; total: number }>>(
        "get",
        baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}/children`),
        { params: { withMountCount: 1 } }
    );

export const getCatalogNode = (id: number) =>
    http.request<BaseResult<CatalogNode>>("get", baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}`));

export const getAnomalyCount = () =>
    http.request<BaseResult<{ count: number }>>("get", baseUrlApi("gb28181/device-mgmt/catalog/anomaly/count"));

// 三维目录树:顶层节点(dimension = native / biz_group / civil_code)
export const listDirectoryRoots = (dimension: DirectoryDimension, withCounts = false) =>
    http.request<BaseResult<{ dimension: string; list: DirectoryNode[]; total: number }>>(
        "get",
        baseUrlApi("gb28181/directory/tree"),
        { params: { dimension, withCounts: withCounts ? 1 : 0 } }
    );

// 三维目录树:子节点
export const listDirectoryChildren = (dimension: DirectoryDimension, parentId: string, withCounts = false) =>
    http.request<BaseResult<{ dimension: string; list: DirectoryNode[]; total: number }>>(
        "get",
        baseUrlApi("gb28181/directory/tree"),
        { params: { dimension, parentId, withCounts: withCounts ? 1 : 0 } }
    );

export const listDevices = (params: DeviceQuery) =>
    http.request<BaseResult<PageResult<DeviceVO>>>("get", baseUrlApi("gb28181/device-mgmt/devices"), { params });

export const getDevice = (id: number) =>
    http.request<BaseResult<DeviceVO>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}`));

export const listChannels = (params: ChannelQuery) =>
    http.request<BaseResult<PageResult<ChannelVO>>>("get", baseUrlApi("gb28181/device-mgmt/channels"), { params });

export const getChannel = (id: number) =>
    http.request<BaseResult<ChannelVO>>("get", baseUrlApi(`gb28181/device-mgmt/channel/${id}`));

export const listChannelMounts = (id: number) =>
    http.request<BaseResult<{ list: ChannelMount[]; total: number }>>(
        "get",
        baseUrlApi(`gb28181/device-mgmt/channel/${id}/mounts`)
    );

export const getChannelTimeline = (id: number) =>
    http.request<BaseResult<{ slots: TimelineSlot[]; range: string; phase1Simplified: boolean }>>(
        "get",
        baseUrlApi(`gb28181/device-mgmt/channel/${id}/timeline`)
    );

export const listMapMarkers = (params: { limit?: number } = {}) =>
    http.request<BaseResult<{ list: MapMarker[]; total: number }>>(
        "get",
        baseUrlApi("gb28181/device-mgmt/map/markers"),
        { params }
    );

export const listMapClusters = (params: { zoom: number }) =>
    http.request<BaseResult<{ clusters: MapCluster[]; zoom: number; gridSize: number }>>(
        "get",
        baseUrlApi("gb28181/device-mgmt/map/clusters"),
        { params }
    );

export const getNoCoordCount = () =>
    http.request<BaseResult<{ count: number }>>("get", baseUrlApi("gb28181/device-mgmt/map/no-coord-count"));

export const listAnomalies = (params: { resolved?: "0" | "1"; page?: number; pageSize?: number }) =>
    http.request<BaseResult<PageResult<AnomalyRecord>>>("get", baseUrlApi("gb28181/device-mgmt/anomaly"), {
        params
    });

export const resolveAnomaly = (id: number, note?: string) =>
    http.request<BaseResult<{ id: number; ok: boolean }>>(
        "post",
        baseUrlApi(`gb28181/device-mgmt/anomaly/${id}/resolve`),
        { data: { action: "mark-resolved", note } }
    );

export const batchResolveAnomalies = (ids: number[]) =>
    http.request<BaseResult<{ succeeded: number[]; failed: Array<{ id: number; error: string }> }>>(
        "post",
        baseUrlApi("gb28181/device-mgmt/anomaly/batch-resolve"),
        { data: { ids, action: "mark-resolved" } }
    );

export interface BatchDeleteResult {
    succeeded: number[];
    failed: Array<{ id: number; error: string }>;
}

export const deleteDevice = (id: number) =>
    http.request<BaseResult<{ id: number; ok: boolean }>>(
        "delete",
        baseUrlApi(`gb28181/device-mgmt/device/${id}`)
    );

export const batchDeleteDevices = (ids: number[]) =>
    http.request<BaseResult<BatchDeleteResult>>(
        "post",
        baseUrlApi("gb28181/device-mgmt/device/batch-delete"),
        { data: { ids } }
    );

export const deleteChannel = (id: number) =>
    http.request<BaseResult<{ id: number; ok: boolean }>>(
        "delete",
        baseUrlApi(`gb28181/device-mgmt/channel/${id}`)
    );

export const batchDeleteChannels = (ids: number[]) =>
    http.request<BaseResult<BatchDeleteResult>>(
        "post",
        baseUrlApi("gb28181/device-mgmt/channel/batch-delete"),
        { data: { ids } }
    );

export const refreshDeviceCatalog = (id: number) =>
    http.request<BaseResult<{ deviceId: string; dest: string; ok: boolean }>>(
        "post",
        baseUrlApi(`gb28181/device-mgmt/device/${id}/catalog/refresh`)
    );

export type StreamTransport = "UDP" | "TCP-Active" | "TCP-Passive";

export const updateChannelStreamTransport = (id: number, streamTransport: StreamTransport) =>
    http.request<BaseResult<{ id: number; streamTransport: string }>>(
        "patch",
        baseUrlApi(`gb28181/device-mgmt/channel/${id}/stream-transport`),
        { data: { streamTransport } }
    );
