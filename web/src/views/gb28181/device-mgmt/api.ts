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

export interface DeviceVO {
    id: number;
    deviceId: string;
    name: string;
    alias: string;
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

export type DeviceStatusEventType =
    | "register_online"
    | "unregister_offline"
    | "heartbeat_timeout"
    | "heartbeat_recovered"
    | "register_renewed";

export interface DeviceStatusEvent {
    id: number;
    deviceId: number;
    deviceCode: string;
    eventType: DeviceStatusEventType;
    eventName: string;
    fromStatus: number | null;
    toStatus: number;
    occurredAt: string;
    source: "register" | "unregister" | "keepalive" | "offline_scanner";
    registerExpires: number | null;
    keepaliveInterval: number | null;
    ip: string;
    port: number;
    transport: string;
}

export interface DeviceStatusEventQuery {
    page?: number;
    pageSize?: number;
    eventType?: DeviceStatusEventType;
    from?: string;
    to?: string;
}

export type SubscriptionKind = "catalog" | "mobile_position" | "alarm";
export type SubscriptionStatus = "disabled" | "pending" | "active" | "degraded" | "expired";

export interface DeviceSubscription {
    kind: SubscriptionKind;
    enabled: boolean;
    status: SubscriptionStatus;
    expiresSeconds: number;
    intervalSeconds: number;
    expiresAt?: string | null;
    lastNotifyAt?: string | null;
    lastError: string;
}

export interface ChannelVO {
    id: number;
    channelId: string;
    deviceId: string;
    name: string;
    alias: string;
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
    deviceId?: string;
    nodeId?: number;
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

export const listDevices = (params: DeviceQuery) =>
    http.request<BaseResult<PageResult<DeviceVO>>>("get", baseUrlApi("gb28181/device-mgmt/devices"), { params });

export const getDevice = (id: number) =>
    http.request<BaseResult<DeviceVO>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}`));

export const listDeviceSubscriptions = (id: number) =>
    http.request<BaseResult<{ list: DeviceSubscription[] }>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions`));

export const updateDeviceSubscription = (id: number, kind: SubscriptionKind, enabled: boolean) =>
    http.request<BaseResult<DeviceSubscription>>("patch", baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions/${kind}`), { data: { enabled } });

export const renewDeviceSubscription = (id: number, kind: SubscriptionKind) =>
    http.request<BaseResult<DeviceSubscription>>("post", baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions/${kind}/renew`));

export const listDeviceStatusEvents = (id: number, params: DeviceStatusEventQuery = {}) =>
    http.request<BaseResult<PageResult<DeviceStatusEvent>>>(
        "get",
        baseUrlApi(`gb28181/device-mgmt/device/${id}/status-events`),
        { params }
    );

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

export interface MapQuery {
    limit?: number;
    zoom?: number;
    minLat?: number;
    maxLat?: number;
    minLng?: number;
    maxLng?: number;
    q?: string;
    nodeId?: number;
    status?: OnlineStatus;
}

export const listMapMarkers = (params: MapQuery = {}) =>
    http.request<BaseResult<{ list: MapMarker[]; total: number }>>(
        "get",
        baseUrlApi("gb28181/device-mgmt/map/markers"),
        { params }
    );

export const listMapClusters = (params: MapQuery & { zoom: number }) =>
    http.request<BaseResult<{ clusters: MapCluster[]; zoom: number; gridSize: number }>>(
        "get",
        baseUrlApi("gb28181/device-mgmt/map/clusters"),
        { params }
    );

export const getNoCoordCount = (params: Pick<MapQuery, "q" | "nodeId" | "status"> = {}) =>
    http.request<BaseResult<{ count: number }>>("get", baseUrlApi("gb28181/device-mgmt/map/no-coord-count"), { params });

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

export interface CreateDeviceDTO {
    deviceId: string;
    name?: string;
    password?: string;
    transport?: "UDP" | "TCP";
}

export const createDevice = (data: CreateDeviceDTO) =>
    http.request<BaseResult<{ id: number; deviceId: string }>>(
        "post",
        baseUrlApi("gb28181/device-mgmt/device"),
        { data }
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

export const updateChannel = (id: number, data: { alias?: string; ptzType?: number }) =>
    http.request<BaseResult<{ id: number; updates: Record<string, unknown> }>>(
        "patch",
        baseUrlApi(`gb28181/device-mgmt/channel/${id}`),
        { data }
    );

export const updateDevice = (deviceId: string, data: { alias?: string; manufacturer?: string; model?: string; firmware?: string }) =>
    http.request<BaseResult<{ deviceId: string }>>(
        "patch",
        baseUrlApi(`gb28181/device/${deviceId}`),
        { data }
    );
