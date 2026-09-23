import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";
import {
  mockQueryDeviceRecords,
  mockRecordQueryOptions,
  resolveRecordQueryMockScenario,
  shouldUseRecordQueryMock
} from "./recordQueryMock";
import { recordQueryOptionsPath, recordQueryPath } from "./recordQueryState";

export type OnlineStatus = "online" | "offline";
export type AssetKind = "channel" | "device";
export type ProtocolOverride = "auto" | "2016" | "2022";
export type ProtocolVersionSource = "register" | "override" | "history" | "default" | string;
export type DirectoryView = "national" | "administrative" | "business" | "custom";

export interface DirectoryNode {
  key: string;
  name: string;
  type: "area" | "organization" | "biz_group" | "virtual_org" | "unknown" | "group" | "ungrouped" | string;
  code?: string;
  readOnly: boolean;
  count: number;
  onlineCount: number;
  depth: number;
  children?: DirectoryNode[];
}

export interface DirectoryQuery {
  directoryView?: DirectoryView;
  directoryKey?: string;
}

export interface CustomGroup {
  id: number;
  ownerDeptId: number;
  parentId: number;
  path: string;
  depth: number;
  name: string;
  createdBy: number;
  createdAt: string;
  updatedAt: string;
}

export interface PageResult<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface DevicePageResult extends PageResult<DeviceVO> {
  onlineTotal: number;
  offlineTotal: number;
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
  /** Raw X-GB-Ver reported by the device, if any. */
  reportedVersion?: string | null;
  reportedVersionAt?: string | null;
  /** Manual override or auto resolution mode. */
  protocolOverride?: ProtocolOverride | string;
  effectiveVersion?: "2016" | "2022" | string;
  effectiveVersionSource?: ProtocolVersionSource;
  effectiveVersionAt?: string | null;
  /** Preferred ZLM node; 0 lets the cluster scheduler decide. */
  zlmNodeId?: number;
  ownerDeptId: number;
  ownerDeptName?: string;
}

export type DeviceOperationStatus =
  | "queued"
  | "pending"
  | "sent"
  | "accepted"
  | "rejected"
  | "cancelled"
  | "failed"
  | "timeout"
  | "unknown"
  | string;

/** 设备级操作响应。sent 只代表平台已发出请求，不代表设备已完成重启。 */
export interface DeviceOperationResult {
  operationId?: string;
  action: string;
  sn?: number;
  status?: DeviceOperationStatus;
  responseRequired?: boolean;
  deadlineAt?: string | null;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
  profileVersion?: "2016" | "2022" | string;
  deduplicated?: boolean;
  errorMessage?: string | null;
  completedAt?: string | null;
}

export interface MaintenanceOperation {
  operationId: string;
  action: "teleboot" | string;
  status: DeviceOperationStatus;
  sipStatus?: number | null;
  errorMessage?: string | null;
  actorId?: number | null;
  createdAt: string;
  sentAt?: string | null;
  completedAt?: string | null;
  responseRequired?: boolean;
  targetCode?: string | null;
}

export interface MaintenanceOperationsQuery {
  page?: number;
  pageSize?: number;
}

export type FirmwareUpgradeStatus = "queued" | "sent" | "accepted" | "succeeded" | "failed" | "rejected" | "unknown" | string;

export interface UpgradeOperation {
  operationId: string;
  sessionId: string;
  deviceId: number;
  status: FirmwareUpgradeStatus;
  firmware: string;
  currentFirmware?: string | null;
  manufacturer: string;
  sipStatus?: number | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  failedReason?: string | null;
  actorId?: number | null;
  createdAt: string;
  sentAt?: string | null;
  acceptedAt?: string | null;
  completedAt?: string | null;
  deadlineAt?: string | null;
  deduplicated?: boolean;
}

export interface FirmwareUpgradeRequest {
  confirmed: true;
  idempotencyKey: string;
  firmware: string;
  fileUrl: string;
  manufacturer: string;
}

export type FirmwareUpgradeQuery = MaintenanceOperationsQuery;

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

export type SubscriptionKind = "catalog" | "mobile_position" | "alarm" | "ptz_precise_position";
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
  // ---- 设备上报的通道属性(GB/T 28181 附录 A / §9.3.1)----
  // ⛔ 0 / '' 一律表示"设备未上报该属性",不是"属性为 0"。
  // 两版共有的四项在 Catalog Item 的 <Info> 容器内:
  roomType: number; // 1-室外 2-室内(2016/2022 编码一致)
  supplyLightType: number; // 1-无补光 2-红外 3-白光;2022 新增 4-激光 9-其他
  directionType: number; // 1-东 2-西 3-南 4-北 5-东南 6-东北 7-西南 8-西北(两版一致)
  resolution: string; // 如 1920*1080
  // 两版共有的两项在 Catalog Item 层:
  ipAddress: string;
  port: number;
  // 版本独有四项 —— 哪一组有值就说明设备报的是哪一版目录形态:
  positionType: number; // 2016 独有:1-省际检查站 … 10-交通干线
  useType: number; // 2016 独有:1-治安 2-交通 3-重点
  photoelectricImagingType: string; // 2022 独有,可多值 "/" 分隔
  capturePositionType: string; // 2022 独有,见 2022 附录 O
  longitude: number;
  latitude: number;
  // 坐标来源: catalog(目录应答) / mobile(位置订阅实时回写) / manual(人工录入),
  // 空串表示"尚无坐标"。三路写的是同一对 longitude/latitude 列,这里只做来源标注。
  positionSource: string;
  positionUpdatedAt?: string | null;
  status: number;
  streamId: string;
  onDemandLive: boolean; // 无人观看时是否自动关闭
  streamTransport: string; // 流传输模式: UDP / TCP-Active / TCP-Passive
  audioEnabled: boolean; // 点播是否接收音频
  cloudRecordingEnabled: boolean;
  cloudRecordingState: string;
  cloudRecordingError: string;
  cloudRecordingUpdatedAt?: string | null;
  snapshotUrl?: string | null; // 最新快照 URL(相对路径 /public/gb-channel-snapshot/...)
  snapshotAt?: string | null; // 最新快照抓拍时间(ISO 8601)
  createdAt: string;
  updatedAt: string;
}

export type RecordQueryRequestType = "all" | "manual" | "alarm";
export type RecordQueryResultStatus = "complete" | "empty" | "partial";
export type RecordQueryPartialReason = "deadline" | "capacity";
export type RecordQueryErrorCode =
  | "record_query_target_not_found"
  | "record_query_device_offline"
  | "record_query_invalid_argument"
  | "record_query_busy"
  | "record_query_send_failed"
  | "record_query_unavailable"
  | "record_query_timeout";

export interface RecordQueryOptions {
  device: { id: number; code: string; name: string; online: boolean };
  channel: { id: number; code: string; name: string };
  timezone: string;
  serverNow: string;
  maxRangeHours: number;
  timeoutSeconds: number;
  supportedTypes: RecordQueryRequestType[];
}

export interface RecordQueryRequest {
  startTime: string;
  endTime: string;
  type: RecordQueryRequestType;
  secrecy: number;
  recorderId: string;
}

export interface RecordQueryItem {
  recordKey: string;
  deviceId: string;
  name: string | null;
  filePath: string | null;
  address: string | null;
  startTime: string | null;
  endTime: string | null;
  secrecy: number | null;
  type: string | null;
  recorderId: string | null;
  fileSize: number | null;
  recordLocation: string | null;
  streamNumber: number | null;
}

export interface RecordQueryResult {
  status: RecordQueryResultStatus;
  partialReason?: RecordQueryPartialReason | null;
  declaredTotal: number;
  receivedCount: number;
  incomplete: boolean;
  timezone: string;
  elapsedMs: number;
  list: RecordQueryItem[];
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
  /** 与通道列表同口径的坐标来源(见 ChannelVO.positionSource)；地图与列表同源同一份数据。 */
  positionSource: string;
  /** 设备最近一次**位置上报**的时间。⛔ 不等于"坐标更新时间" —— 人工/目录坐标没有上报。 */
  positionUpdatedAt?: string | null;
  /** 位置上报超过 90 秒未更新。⛔ 只说"实时位置不再新鲜"，与坐标本身是否有效无关。 */
  positionStale?: boolean;
}

/**
 * 聚合里"落单的那一个通道"的身份。刻意不等于 {@link MapMarker} ——
 * 后端 clusters 接口不查位置表,所以这里没有来源/新鲜度,别拿它去渲染详情面板。
 */
export interface MapClusterSingle {
  id: number;
  channelId: string;
  name: string;
  status: number;
}

export interface MapCluster {
  centerLat: number;
  centerLng: number;
  count: number;
  onlineCount: number;
  onlineRate: number;
  /** 簇的包围盒。点击聚合要"刚好框住这一簇"再放大:只看质心的话,放大后散在四周的点会跑出视野。 */
  minLat: number;
  maxLat: number;
  minLng: number;
  maxLng: number;
  /**
   * 只在 count === 1 时存在。落单的通道**不是聚合** —— 行业惯例是 count==1 直接画成
   * 通道标记点,而不是一个写着 "1" 的气泡(那会让用户以为平台聚合了不该聚合的东西)。
   */
  single?: MapClusterSingle | null;
}

export interface DeviceQuery extends DirectoryQuery {
  q?: string;
  nodeId?: number;
  status?: OnlineStatus;
  vendor?: string;
  page?: number;
  pageSize?: number;
  sort?: string;
  assignment?: "all" | "unassigned" | "assigned";
  ownerDeptId?: number;
}

export interface ChannelQuery extends DirectoryQuery {
  q?: string;
  deviceId?: string;
  nodeId?: number;
  status?: OnlineStatus;
  ptz?: "1";
  page?: number;
  pageSize?: number;
}

export const listCatalogRoots = () =>
  http.request<BaseResult<{ list: CatalogNode[]; total: number }>>("get", baseUrlApi("gb28181/device-mgmt/catalog/tree"));

export const listDirectoryTree = (view: DirectoryView) =>
  http.request<BaseResult<{ list: DirectoryNode[] }>>("get", baseUrlApi("gb28181/device-mgmt/directory/tree"), {
    params: { view }
  });

export const createCustomGroup = (data: { name: string; parentId?: number | null }) =>
  http.request<BaseResult<CustomGroup>>("post", baseUrlApi("gb28181/device-mgmt/custom-groups"), { data });

export const renameCustomGroup = (id: number, name: string) =>
  http.request<BaseResult<{ id: number; name: string }>>("patch", baseUrlApi(`gb28181/device-mgmt/custom-groups/${id}`), {
    data: { name }
  });

export const moveCustomGroup = (id: number, targetParentId: number | null) =>
  http.request<BaseResult<{ id: number; parentId: number }>>("post", baseUrlApi(`gb28181/device-mgmt/custom-groups/${id}/move`), {
    data: { targetParentId }
  });

export const deleteCustomGroup = (id: number) =>
  http.request<BaseResult<{ removedDeviceCount: number }>>("delete", baseUrlApi(`gb28181/device-mgmt/custom-groups/${id}`));

export const addDevicesToGroup = (id: number, deviceIds: number[]) =>
  http.request<BaseResult<{ requestedCount: number; addedCount: number; skippedCount: number }>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/custom-groups/${id}/devices`),
    { data: { deviceIds } }
  );

export const removeDevicesFromGroup = (id: number, deviceIds: number[]) =>
  http.request<BaseResult<{ requestedCount: number; removedCount: number; skippedCount: number }>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/custom-groups/${id}/devices/remove`),
    { data: { deviceIds } }
  );

export const listCatalogChildren = (id: number) =>
  http.request<BaseResult<{ list: CatalogNode[]; total: number }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}/children`),
    { params: { withMountCount: 1 } }
  );

export const getCatalogNode = (id: number) =>
  http.request<BaseResult<CatalogNode>>("get", baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}`));

export const listDevices = (params: DeviceQuery) =>
  http.request<BaseResult<DevicePageResult>>("get", baseUrlApi("gb28181/device-mgmt/devices"), { params });

export const getDevice = (id: number) =>
  http.request<BaseResult<DeviceVO>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}`));

export const rebootDevice = (id: number, data: { confirmed: true; idempotencyKey: string }) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/device/${id}/reboot`), { data });

export const listMaintenanceOperations = (id: number, params: MaintenanceOperationsQuery = {}) =>
  http.request<BaseResult<PageResult<MaintenanceOperation>>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/device/${id}/maintenance-operations`),
    { params }
  );

export const upgradeDeviceFirmware = (id: number, data: FirmwareUpgradeRequest) =>
  http.request<BaseResult<UpgradeOperation>>("post", baseUrlApi(`gb28181/device-mgmt/device/${id}/firmware-upgrade`), { data });

export const listFirmwareUpgrades = (id: number, params: FirmwareUpgradeQuery = {}) =>
  http.request<BaseResult<PageResult<UpgradeOperation>>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/device/${id}/firmware-upgrades`),
    { params }
  );

export const listDeviceSubscriptions = (id: number) =>
  http.request<BaseResult<{ list: DeviceSubscription[] }>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions`));

export interface SubscriptionUpdate {
  enabled?: boolean;
  expiresSeconds?: number;
  intervalSeconds?: number;
}

export const updateDeviceSubscription = (id: number, kind: SubscriptionKind, data: SubscriptionUpdate) =>
  http.request<BaseResult<DeviceSubscription>>("patch", baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions/${kind}`), {
    data
  });

export const renewDeviceSubscription = (id: number, kind: SubscriptionKind) =>
  http.request<BaseResult<DeviceSubscription>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/device/${id}/subscriptions/${kind}/renew`)
  );

export const listDeviceStatusEvents = (id: number, params: DeviceStatusEventQuery = {}) =>
  http.request<BaseResult<PageResult<DeviceStatusEvent>>>("get", baseUrlApi(`gb28181/device-mgmt/device/${id}/status-events`), {
    params
  });

export const listChannels = (params: ChannelQuery) =>
  http.request<BaseResult<PageResult<ChannelVO>>>("get", baseUrlApi("gb28181/device-mgmt/channels"), { params });

export const getChannel = (id: number) =>
  http.request<BaseResult<ChannelVO>>("get", baseUrlApi(`gb28181/device-mgmt/channel/${id}`));

export const getRecordQueryOptions = (id: number) => {
  if (shouldUseRecordQueryMock(import.meta.env)) return mockRecordQueryOptions(id);
  return http.request<BaseResult<RecordQueryOptions>>("get", baseUrlApi(recordQueryOptionsPath(id)));
};

export const queryDeviceRecords = (id: number, data: RecordQueryRequest, signal?: AbortSignal) => {
  if (shouldUseRecordQueryMock(import.meta.env)) {
    return mockQueryDeviceRecords(id, data, resolveRecordQueryMockScenario(), signal);
  }
  return http.request<BaseResult<RecordQueryResult>>("post", baseUrlApi(recordQueryPath(id)), { data, signal });
};

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

export interface MapQuery extends DirectoryQuery {
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
  http.request<BaseResult<{ list: MapMarker[]; total: number }>>("get", baseUrlApi("gb28181/device-mgmt/map/markers"), {
    params
  });

export const listMapClusters = (params: MapQuery & { zoom: number }) =>
  http.request<BaseResult<{ clusters: MapCluster[]; zoom: number; gridSize: number }>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/map/clusters"),
    { params }
  );

export interface BatchDeleteResult {
  succeeded: number[];
  failed: Array<{ id: number; error: string }>;
}

export const deleteDevice = (id: number) =>
  http.request<BaseResult<{ id: number; ok: boolean }>>("delete", baseUrlApi(`gb28181/device-mgmt/device/${id}`));

export const batchDeleteDevices = (ids: number[]) =>
  http.request<BaseResult<BatchDeleteResult>>("post", baseUrlApi("gb28181/device-mgmt/device/batch-delete"), { data: { ids } });

export const deleteChannel = (id: number) =>
  http.request<BaseResult<{ id: number; ok: boolean }>>("delete", baseUrlApi(`gb28181/device-mgmt/channel/${id}`));

export const batchDeleteChannels = (ids: number[]) =>
  http.request<BaseResult<BatchDeleteResult>>("post", baseUrlApi("gb28181/device-mgmt/channel/batch-delete"), { data: { ids } });

export interface CreateDeviceDTO {
  deviceId: string;
  name?: string;
  password?: string;
  transport?: "UDP" | "TCP";
}

export const createDevice = (data: CreateDeviceDTO) =>
  http.request<BaseResult<{ id: number; deviceId: string }>>("post", baseUrlApi("gb28181/device-mgmt/device"), { data });

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

export const updateChannel = (
  id: number,
  data: {
    alias?: string;
    ptzType?: number;
    audioEnabled?: boolean;
    onDemandLive?: boolean;
    // 人工录入的通道坐标。⛔ 必须成对出现：单给一个后端会拒(400)。
    // 两者同为 0 = 清除坐标；不传这两个字段 = 不动坐标。
    longitude?: number;
    latitude?: number;
  }
) =>
  http.request<BaseResult<{ id: number; updates: Record<string, unknown> }>>(
    "patch",
    baseUrlApi(`gb28181/device-mgmt/channel/${id}`),
    { data }
  );

export const updateCloudRecording = (id: number, enabled: boolean) =>
  http.request<BaseResult<ChannelVO>>("patch", baseUrlApi(`gb28181/device-mgmt/channel/${id}/cloud-recording`), {
    data: { enabled }
  });

export const updateDevice = (
  deviceId: string,
  data: {
    alias?: string;
    manufacturer?: string;
    model?: string;
    firmware?: string;
    protocolOverride?: ProtocolOverride;
    zlmNodeId?: number;
  }
) =>
  http.request<BaseResult<Partial<DeviceVO> & { deviceId: string }>>("patch", baseUrlApi(`gb28181/device/${deviceId}`), { data });
