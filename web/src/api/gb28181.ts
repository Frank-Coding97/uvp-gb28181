import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import { BaseResult } from "./types";
import { getAccessToken } from "@/utils/auth";
import type { ChannelVO } from "@/views/gb28181/device-mgmt/api";

const silentRequestConfig = { showErrorMessage: false };

// ===== 设备 =====

export interface GbDevice {
  id: number;
  deviceId: string;
  name: string;
  transport: string;
  manufacturer: string;
  model: string;
  firmware: string;
  ip: string;
  port: number;
  status: number; // 0 离线 1 在线
  online: boolean; // 从事实派生
  keepaliveTime: string | null;
  registerTime: string | null;
  reportedVersion?: string | null;
  reportedVersionAt?: string | null;
  protocolOverride?: "auto" | "2016" | "2022" | string;
  effectiveVersion?: "2016" | "2022" | string;
  effectiveVersionSource?: "register" | "override" | "history" | "default" | string;
  effectiveVersionAt?: string | null;
}

export interface GbChannel {
  id: number;
  deviceId: string;
  channelId: string;
  name: string;
  manufacturer: string;
  model: string;
  parentId: string;
  status: number;
  streamId: string;
  /** 通道最新快照 URL(相对路径 /public/gb-channel-snapshot/...);无为空字符串或 null */
  snapshotUrl?: string | null;
  /** 通道最新快照抓拍时间(ISO 8601);无为 null */
  snapshotAt?: string | null;
}

export type DeviceListResult = BaseResult<{
  list: GbDevice[];
  total: number;
  page: number;
  pageSize: number;
}>;

export type ChannelListResult = BaseResult<{
  list: GbChannel[];
  total: number;
}>;

// ===== 通道收藏 =====

export interface ChannelFavoriteInput { deviceCode: string; channelCode: string; }
export interface ChannelFavoriteItem { id: number; deviceCode: string; channelCode: string; deviceName: string; channelName: string; channel?: ChannelVO; }
export interface ChannelFavoriteGroup { id: number; name: string; items: ChannelFavoriteItem[]; availableCount: number; unavailableCount: number; }
export interface ChannelFavoriteAppendResult { requestedCount: number; addedCount: number; skippedCount: number; }

const channelFavoriteGroupsPath = "gb28181/channel-favorite-groups";
export const listChannelFavoriteGroups = () => http.request<BaseResult<{ list: ChannelFavoriteGroup[] }>>("get", baseUrlApi(channelFavoriteGroupsPath));
export const createChannelFavoriteGroup = (name: string, channels: ChannelFavoriteInput[]) => http.request<BaseResult<ChannelFavoriteGroup>>("post", baseUrlApi(channelFavoriteGroupsPath), { data: { name, channels } });
export const appendChannelFavoriteGroup = (groupId: number, channels: ChannelFavoriteInput[]) => http.request<BaseResult<ChannelFavoriteAppendResult>>("post", baseUrlApi(`${channelFavoriteGroupsPath}/${groupId}/channels`), { data: { channels } });
export const removeChannelFavoriteItem = (groupId: number, channel: ChannelFavoriteInput) => http.request<BaseResult<{ removed: boolean }>>("delete", baseUrlApi(`${channelFavoriteGroupsPath}/${groupId}/channels`), { data: channel });
export const deleteChannelFavoriteGroup = (groupId: number) => http.request<BaseResult<{ deleted: boolean }>>("delete", baseUrlApi(`${channelFavoriteGroupsPath}/${groupId}`));

// ===== 点播 =====

export interface PlayResult {
  streamId: string;
  ssrc: string;
  app: string;
  reused?: boolean;
  status?: string;
  node?: { id: number; name: string; host: string };
  urls?: {
    wsFlv?: string | null;
    httpFlv?: string | null;
    wssFlv?: string | null;
    httpsFlv?: string | null;
    wsFmp4?: string | null;
    httpFmp4?: string | null;
    wssFmp4?: string | null;
    httpsFmp4?: string | null;
    hls?: string | null;
    httpsHls?: string | null;
    wsTs?: string | null;
    httpTs?: string | null;
    wssTs?: string | null;
    httpsTs?: string | null;
    webrtc?: string | null;
    webrtcs?: string | null;
    rtmp?: string | null;
    rtmps?: string | null;
    rtsp?: string | null;
    rtsps?: string | null;
  };
  urlWarnings?: string[];
  defaultProtocol?: PlaybackProtocol;
  protocol?: PlaybackProtocol;
  url?: string;
  zlmWebrtc?: boolean;
  wsflvUrl: string;
  httpFlvUrl: string;
  hlsUrl: string;
  expireAt: number;
  authorizationExpiresAt?: number;
}

export type PlayApiResult = BaseResult<PlayResult>;

// ===== API =====

/** 设备列表(分页) */
export const listDevices = (params: { page?: number; pageSize?: number } = {}) =>
  http.request<DeviceListResult>("get", baseUrlApi("gb28181/device/list"), { params });

/** 某设备的通道列表 */
export const listChannels = (deviceId: string) =>
  http.request<ChannelListResult>("get", baseUrlApi(`gb28181/device/${deviceId}/channels`));

/** 发起点播 */
export const startPlay = (deviceId: string, channelId: string, options: { silent?: boolean } = {}) => {
  const url = baseUrlApi(`gb28181/play/${deviceId}/${channelId}`);
  return options.silent
    ? http.request<PlayApiResult>("post", url, undefined, silentRequestConfig)
    : http.request<PlayApiResult>("post", url);
};

/** 为固定播放地址刷新短期访问凭据。 */
export const authorizeFixedPlayback = (deviceId: string, channelId: string) =>
  http.request<PlayApiResult>("post", baseUrlApi(`gb28181/play/${deviceId}/${channelId}/authorization`), undefined, silentRequestConfig);

/**
 * 停播响应
 * - released=true:录像收尾后完成通道级停流,前端刷新列表清"直播中"徽章
 */
export interface StopPlayResult {
  released: boolean;
  streamId: string;
}

/** 停播 */
export const stopPlay = (streamId: string) =>
  http.request<BaseResult<StopPlayResult>>("delete", baseUrlApi(`gb28181/play/${streamId}`));

// ===== 国标级联 =====

export type CascadeProfileOverride = "auto" | "2016" | "2022" | string;
export type CascadeRegistrationState = "unregistered" | "registered" | "expired" | string;
export type CascadeHeartbeatState = "unknown" | "healthy" | "stale" | string;
export type CascadeOverallState = "online" | "offline" | string;

export interface CascadePlatform {
  id: number;
  name: string;
  upstreamServerId: string;
  upstreamDomain: string;
  host: string;
  port: number;
  localDeviceId: string;
  localDomain: string;
  localSipIp: string;
  localSipPort: number;
  mediaAdvertiseIp?: string;
  authUsername?: string;
  hasPassword: boolean;
  profileOverride: CascadeProfileOverride;
  effectiveVersion: string;
  effectiveVersionFrom: string;
  charsetOverride?: string;
  registerExpires: number;
  keepaliveInterval: number;
  transport: "UDP" | "TCP" | string;
  catalogBatchSize: number;
  publishPlatform: boolean;
  publishCivil: boolean;
  publishGroup: boolean;
  maxStreams: number;
  ptzEnabled: boolean;
  enabled: boolean;
  configRevision: number;
  projectionRevision: number;
  registration: CascadeRegistrationState;
  heartbeat: CascadeHeartbeatState;
  overall: CascadeOverallState;
  registerAt?: string | null;
  registerExpiresAt?: string | null;
  heartbeatAt?: string | null;
  lastErrorCode?: string;
  lastErrorMessage?: string;
  lastErrorAt?: string | null;
}

export type CascadePlatformInput = Omit<
  CascadePlatform,
  | "id"
  | "hasPassword"
  | "effectiveVersion"
  | "effectiveVersionFrom"
  | "configRevision"
  | "projectionRevision"
  | "registration"
  | "heartbeat"
  | "overall"
  | "registerAt"
  | "registerExpiresAt"
  | "heartbeatAt"
  | "lastErrorCode"
  | "lastErrorMessage"
  | "lastErrorAt"
> & { password?: string; retryPolicy?: string };

export interface CascadeDeviceProjection {
  id?: number;
  platformId?: number;
  sourceDeviceId: number;
  publishedDeviceId: string;
  name: string;
  active?: boolean;
}

export interface CascadeChannelProjection {
  id?: number;
  platformId?: number;
  deviceProjectionId?: number;
  sourceDeviceId: number;
  sourceChannelId: number;
  publishedChannelId: string;
  name: string;
  parentOverride: string;
  ptzAllowed: boolean;
  active?: boolean;
}

export interface CascadeShares {
  platformId: number;
  revision: number;
  devices: CascadeDeviceProjection[];
  channels: CascadeChannelProjection[];
}

export interface CascadeListData { list: CascadePlatform[] }

export const listCascadePlatforms = () =>
  http.request<CascadeListData>("get", baseUrlApi("gb28181/cascade/platforms"));

export const getCascadePlatform = (id: number) =>
  http.request<CascadePlatform>("get", baseUrlApi(`gb28181/cascade/platforms/${id}`));

export const createCascadePlatform = (data: CascadePlatformInput) =>
  http.request<CascadePlatform>("post", baseUrlApi("gb28181/cascade/platforms"), { data });

export const updateCascadePlatform = (id: number, data: CascadePlatformInput, expectedRevision: number) =>
  http.request<CascadePlatform>("put", baseUrlApi(`gb28181/cascade/platforms/${id}`), {
    data: { ...data, expectedRevision }
  });

export const deleteCascadePlatform = (id: number) =>
  http.request<{ ok: boolean }>("delete", baseUrlApi(`gb28181/cascade/platforms/${id}`));

export const setCascadePlatformEnabled = (id: number, enabled: boolean, expectedRevision: number) =>
  http.request<CascadePlatform>("put", baseUrlApi(`gb28181/cascade/platforms/${id}/enabled`), {
    data: { enabled, expectedRevision }
  });

export const reconnectCascadePlatform = (id: number) =>
  http.request<{ ok: boolean; platformId: number }>("post", baseUrlApi(`gb28181/cascade/platforms/${id}/reconnect`));

export const getCascadeShares = (id: number) =>
  http.request<CascadeShares>("get", baseUrlApi(`gb28181/cascade/platforms/${id}/shares`));

export const replaceCascadeShares = (id: number, data: {
  scope: "all" | "devices" | "channels";
  devices: CascadeDeviceProjection[];
  channels: CascadeChannelProjection[];
}) => http.request<CascadeShares>("put", baseUrlApi(`gb28181/cascade/platforms/${id}/shares`), { data });

// ===== 多屏播放方案 =====

export type PlaybackSchemeLayoutSize = 1 | 4 | 6 | 8 | 9 | 16;

export interface PlaybackSchemeSlotInput {
  slotIndex: number;
  deviceCode: string;
  channelCode: string;
}

export interface PlaybackSchemeSummary {
  id: number;
  name: string;
  layoutSize: PlaybackSchemeLayoutSize;
  slotCount: number;
  updatedAt: string;
}

export type PlaybackSchemeAvailability = "available" | "offline" | "missing" | "forbidden";

export interface PlaybackSchemeSlot extends PlaybackSchemeSlotInput {
  id: number;
  deviceName: string;
  channelName: string;
  availability: PlaybackSchemeAvailability;
  channelRecordId: number | null;
  channelStatus: number | null;
  audioEnabled: boolean;
}

export interface PlaybackSchemeDetail extends PlaybackSchemeSummary {
  slots: PlaybackSchemeSlot[];
}

export interface PlaybackSchemeListData {
  list: PlaybackSchemeSummary[];
  total: number;
  page: number;
  pageSize: number;
}

export interface PlaybackSchemePayload {
  name: string;
  layoutSize: PlaybackSchemeLayoutSize;
  slots: PlaybackSchemeSlotInput[];
}

export const listPlaybackSchemes = (params: { page?: number; pageSize?: number; q?: string } = {}) =>
  http.request<BaseResult<PlaybackSchemeListData>>("get", baseUrlApi("gb28181/playback-schemes"), { params });

export const getPlaybackScheme = (id: number) =>
  http.request<BaseResult<PlaybackSchemeDetail>>("get", baseUrlApi(`gb28181/playback-schemes/${id}`));

export const createPlaybackScheme = (data: PlaybackSchemePayload) =>
  http.request<BaseResult<PlaybackSchemeSummary>>("post", baseUrlApi("gb28181/playback-schemes"), { data });

export const renamePlaybackScheme = (id: number, name: string) =>
  http.request<BaseResult<{ id: number; name: string }>>("patch", baseUrlApi(`gb28181/playback-schemes/${id}`), { data: { name } });

export const replacePlaybackSchemeLayout = (id: number, data: Omit<PlaybackSchemePayload, "name">) =>
  http.request<BaseResult<PlaybackSchemeSummary>>("put", baseUrlApi(`gb28181/playback-schemes/${id}/layout`), { data });

export const deletePlaybackScheme = (id: number) =>
  http.request<BaseResult<{ id: number }>>("delete", baseUrlApi(`gb28181/playback-schemes/${id}`));

export interface StreamMonitorTrack {
  kind: "video" | "audio" | "unknown";
  codec: string;
  ready: boolean;
  frames: number;
  duration: number;
  loss: number | null;
  width: number;
  height: number;
  fps: number;
  keyFrames: number;
  gopSize: number;
  gopIntervalMs: number;
  sampleRate: number;
  channels: number;
  sampleBit: number;
}

export interface StreamMonitorSnapshot {
  streamId: string;
  collectedAt: string;
  status: "online" | string;
  node: { id: number; name: string; host: string };
  quality: { bitrateKbps: number };
  network: {
    bytesSpeed: number;
    totalBytes: number;
    readerCount: number;
    totalReaderCount: number;
    aliveSecond: number;
  };
  tracks: StreamMonitorTrack[];
  recording: { mp4: boolean; hls: boolean };
}

export const getStreamMonitor = (streamId: string) =>
  http.request<BaseResult<StreamMonitorSnapshot>>(
    "get",
    baseUrlApi(`gb28181/play/${streamId}/monitor`),
    undefined,
    silentRequestConfig
  );

export interface ProbeSnapshot {
  nodeId: number;
  nodeName: string;
  completedAt: string;
  summary: {
    sampleDurationMs: number;
    frameCount: number;
    totalBytes: number;
    averageBitrateKbps: number;
  };
  video: {
    codec: string;
    frameCount: number;
    keyFrameCount: number;
    fps: number | null;
    gop: number | null;
    averageIntervalMs: number | null;
  } | null;
  audio: {
    codec: string;
    frameCount: number;
    keyFrameCount: number;
    fps: number | null;
    gop: number | null;
    averageIntervalMs: number | null;
  } | null;
  timestamps: {
    videoDtsIntervalMeanMs: number | null;
    arrivalJitterMs: number | null;
    ptsDtsMaxMs: number | null;
    avArrivalSkewMaxMs: number | null;
  };
  timeline: Array<{
    sequence: number;
    trackType: string;
    codec: string;
    keyFrame: boolean;
    configFrame: boolean;
    relativeTimeMs: number;
    frameSize: number;
  }>;
  health: {
    status: "ok" | "warning" | "error" | string;
    issues: Array<{ code: string; message: string; thresholdMs?: number; observedMs?: number }>;
    thresholds: { largeArrivalGapMs: number; keyFrameWindowMs: number };
  };
}

export const runStreamProbe = (streamId: string) =>
  http.request<BaseResult<ProbeSnapshot>>("post", baseUrlApi(`gb28181/play/${streamId}/probe`));

export interface ControlCapability {
  state: "supported" | "unsupported" | "unknown" | string;
  reason: string;
}

export interface DeviceControlCapabilities {
  basicPtz: ControlCapability;
  iFrame: ControlCapability;
  record: ControlCapability;
  guard: ControlCapability;
  alarmReset: ControlCapability;
  teleBoot: ControlCapability;
  dragZoom: ControlCapability;
  broadcast?: ControlCapability;
  talk?: ControlCapability;
}

export const getControlCapabilities = (channelId: number) =>
  http.request<BaseResult<DeviceControlCapabilities>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/control-capabilities`),
    undefined,
    silentRequestConfig
  );

export type DeviceFactState = "on" | "off" | "armed" | "disarmed" | "alarm" | "unknown" | string;
export type DeviceStatusFreshness = "fresh" | "stale" | "unknown" | string;
export type AlarmTargetResolutionStatus = "resolved" | "ambiguous" | "unavailable" | string;

export interface DeviceControlState {
  recordState?: DeviceFactState;
  guardState?: DeviceFactState;
  freshness: DeviceStatusFreshness;
  observedAt?: string | null;
  source?: string;
  sourceSn?: number;
  sourceOperationId?: string | null;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
}

export interface DeviceStatusFact {
  state: DeviceFactState;
  freshness: DeviceStatusFreshness;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
}

export interface DeviceAlarmResolution {
  status: AlarmTargetResolutionStatus;
  source?: string;
  targetCode?: string | null;
  state: DeviceFactState;
  freshness: DeviceStatusFreshness;
  candidates: Array<{ code: string; name?: string }>;
}

export interface DeviceAlarmFact {
  targetCode: string;
  guardState: DeviceFactState;
  freshness: DeviceStatusFreshness;
  observedAt?: string | null;
}

export interface DeviceStatusRefreshOperationIds {
  record?: string | null;
  alarm?: string | null;
}

export interface DeviceStatusResult {
  state?: DeviceControlState | null;
  recordState?: DeviceFactState;
  guardState?: DeviceFactState;
  freshness: DeviceStatusFreshness;
  completeness?: "complete" | "partial" | string;
  record?: DeviceStatusFact | null;
  alarmResolution?: DeviceAlarmResolution | null;
  alarmFacts?: DeviceAlarmFact[];
  refreshOperationId?: string | null;
  recordRefreshOperationId?: string | null;
  alarmRefreshOperationId?: string | null;
  refreshOperationIds?: DeviceStatusRefreshOperationIds | null;
  refreshError?: string | null;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
}

/** DeviceStatus refresh is asynchronous on the SIP side; refresh=true only starts it. */
export const getDeviceStatus = (channelId: number, refresh = false) =>
  http.request<BaseResult<DeviceStatusResult>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/device-status`),
    { params: refresh ? { refresh: true } : undefined },
    silentRequestConfig
  );

export interface DeviceOperationResult {
  operationId?: string;
  channelId?: string;
  action: string;
  id?: number;
  sn?: number;
  status?: PTZOperationStatus | string;
  responseRequired?: boolean;
  deadlineAt?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  deviceResult?: string | null;
  completedAt?: string | null;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
  deduplicated?: boolean;
  profileVersion?: "2016" | "2022" | string;
}

export const controlDevice = (channelId: number, data: Record<string, unknown>) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/device-control`), {
    data
  });

export type DeviceSnapshotState = "creating" | "waiting" | "receiving" | "completed" | "failed";

export interface DeviceSnapshotFile {
  name: string;
  size: number;
  receivedAt: string;
  url: string;
}

export interface DeviceSnapshotSession {
  sessionId: string;
  operationId?: string;
  channelId: string;
  channelCode: string;
  deviceCode: string;
  snapNum: number;
  interval: number;
  state: DeviceSnapshotState;
  receivedCount: number;
  notifiedCount: number;
  files: DeviceSnapshotFile[];
  error?: string;
}

export const createDeviceSnapshotSession = (channelId: number, data: { snapNum: number; interval: number }) =>
  http.request<BaseResult<DeviceSnapshotSession>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/snapshot-sessions`),
    { data, headers: { "Idempotency-Key": `snapshot-${channelId}-${Date.now()}` } }
  );

export const getDeviceSnapshotSession = (channelId: number, sessionId: string) =>
  http.request<BaseResult<DeviceSnapshotSession>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/snapshot-sessions/${encodeURIComponent(sessionId)}`)
  );

export const controlPtz = (channelId: number, data: Record<string, unknown>) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz`), { data });

export const controlPtzPrecise = (channelId: number, data: Record<string, unknown>) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/precise`), {
    data
  });

export const controlPtzExtended = (channelId: number, data: Record<string, unknown>) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/extended`), {
    data
  });

export const createPtzPreset = (channelId: number, data: { presetId: number; name: string; idempotencyKey?: string }) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/presets`),
    { data }
  );

export const callPtzPreset = (channelId: number, presetId: number) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/presets/${presetId}/call`)
  );

export const deletePtzPreset = (channelId: number, presetId: number) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "delete",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/presets/${presetId}`)
  );

export const controlPtzCruise = (
  channelId: number,
  data: { action: "start" | "stop" | "delete"; trackId: number; idempotencyKey?: string }
) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/cruise`),
    { data }
  );

export interface CruiseTrackCreateResult {
  channelId: string;
  trackId: number;
  totalStops: number;
  completedStops: number;
  status: string;
  steps: Array<Record<string, unknown>>;
  reconciled?: boolean;
  reconcileScheduled?: boolean;
  error?: string;
}

export type PTZResourceFreshness = "fresh" | "stale" | "unknown" | string;

export interface CruiseTrackPointResource {
  presetIndex?: number;
  presetId?: number;
  stayTime?: number;
  dwellSec?: number;
  /** Query-side business speed. The standard query range is 1..15. */
  speed?: number | null;
}

/** Explicit query DTO. Do not validate this with the create/control range. */
export type CruiseTrackQueryPoint = CruiseTrackPointResource;

/** Create/control input uses the 12-bit value (1..4095) when present. */
export interface CruiseTrackControlInput {
  trackId: number;
  name?: string;
  speed?: number;
  dwellSec?: number;
  stops: Array<{ presetId: number }>;
  replaceExisting?: boolean;
  idempotencyKey?: string;
}

export interface CruiseTrackDetailResource {
  trackId?: number;
  name?: string;
  sumNum?: number;
  cruisePoints?: CruiseTrackPointResource[];
  stops?: CruiseTrackPointResource[];
  /** Query-side values are separate from the create/control 12-bit value. */
  speed?: number | null;
  dwellSec?: number | null;
  source?: string;
}

export interface CruiseTrackResource {
  id?: number;
  trackId: number;
  name?: string;
  enabled?: boolean | null;
  detail?: string | CruiseTrackDetailResource | null;
  updatedAt?: string;
}

export interface CruiseTrackListResult {
  list: CruiseTrackResource[];
  freshness: PTZResourceFreshness;
  refreshOperationId?: string;
  refreshError?: string;
}

export const createCruiseTrack = (
  channelId: number,
  data: CruiseTrackControlInput
) =>
  http.request<BaseResult<CruiseTrackCreateResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/cruise/tracks`),
    { data }
  );

export const listPtzPresets = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ list: Array<Record<string, unknown>>; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/presets`),
    { params: refresh ? { refresh: true } : undefined },
    silentRequestConfig
  );

export const listCruiseTracks = (channelId: number, refresh = false) =>
  http.request<BaseResult<CruiseTrackListResult>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/cruise-tracks`),
    { params: refresh ? { refresh: true } : undefined },
    silentRequestConfig
  );

export type HomePositionSource = "device_query" | "control_ack" | "legacy_profile";
export type HomePositionVerification = "verified" | "unverified";
export type HomePositionFreshness = "fresh" | "stale" | "unknown";
export type HomePositionSupportStatus = "supported" | "unsupported" | "unknown";
export type PTZOperationStatus = "queued" | "sent" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled";

export interface HomePositionConfig {
  enabled: boolean;
  resetTime: number | null;
  presetId: number | null;
  confirmedAt: string;
  source: HomePositionSource;
  verification: HomePositionVerification;
}

export interface HomePositionSupport {
  status: HomePositionSupportStatus;
  reason: string;
}

export interface HomePositionControl {
  status: "idle" | "pending" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled";
  operationId: string | null;
  action: string | null;
  errorCode: string | null;
  deadlineAt: string | null;
}

export interface HomePositionRefresh {
  status: "idle" | "pending" | "succeeded" | "succeeded_no_data" | "timeout" | "failed";
  operationId: string | null;
  errorCode: string | null;
  deadlineAt: string | null;
}

export interface HomePositionResult {
  homePosition: HomePositionConfig | null;
  controlSupport: HomePositionSupport;
  querySupport: HomePositionSupport;
  freshness: HomePositionFreshness;
  control: HomePositionControl;
  refresh: HomePositionRefresh;
}

export interface PTZOperation {
  operationId: string;
  status: PTZOperationStatus;
  errorCode: string | null;
  errorMessage: string | null;
  responseRequired?: boolean;
  deviceResult?: string | null;
  targetScope?: "channel" | "device" | "alarm" | string;
  targetCode?: string | null;
  deduplicated?: boolean;
  completedAt: string | null;
  deadlineAt: string | null;
}

export type HomePositionPatch =
  | { enabled: false }
  | { enabled: true; resetTime: number; presetId: number };

export interface HomePositionUpdateResult {
  operationId: string;
  sn: number;
  channelId: string;
  action: "home_position";
  status: PTZOperationStatus;
}

export const getHomePosition = (channelId: number, refresh = false, idempotencyKey?: string) =>
  http.request<BaseResult<HomePositionResult>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/home-position`),
    {
      params: refresh ? { refresh: true } : undefined,
      ...(idempotencyKey ? { headers: { "Idempotency-Key": idempotencyKey } } : {})
    },
    silentRequestConfig
  );

export const updateHomePosition = (channelId: number, data: HomePositionPatch, idempotencyKey?: string) =>
  http.request<BaseResult<HomePositionUpdateResult>>(
    "patch",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/home-position`),
    {
      data,
      ...(idempotencyKey ? { headers: { "Idempotency-Key": idempotencyKey } } : {})
    }
  );

export const getPtzOperation = (channelId: number, operationId: string) =>
  http.request<BaseResult<PTZOperation>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/operations/${operationId}`),
    undefined,
    silentRequestConfig
  );

export const getPtzPreciseStatus = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ state: Record<string, unknown> | null; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/precise-status`),
    { params: refresh ? { refresh: true } : undefined }
  );

export interface TalkCreateResult {
  sessionId: string;
  mode: "broadcast" | "talk";
  state: string;
  phase?: string;
  nodeId: number;
  nodeName: string;
  sourceStream: string;
  recvStream: string;
  ssrc: string;
  publishUrl: string;
  publishToken: string;
  expiresAt: string;
}

export interface TalkSessionView {
  sessionId: string;
  mode: "broadcast" | "talk";
  state: string;
  phase?: string;
  expiresAt: string;
  startedAt?: string;
  endedAt?: string;
  error?: string;
}

export const createTalkSession = (channelId: number, mode: "broadcast" | "talk") =>
  http.request<BaseResult<TalkCreateResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/talk-sessions`), {
    data: { mode }
  });

export const getTalkSession = (channelId: number, sessionId: string) =>
  http.request<BaseResult<TalkSessionView>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/talk-sessions/${sessionId}`)
  );

export const deleteTalkSession = (channelId: number, sessionId: string) =>
  http.request<BaseResult<{ sessionId: string; state: string }>>(
    "delete",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/talk-sessions/${sessionId}`)
  );

/** 更新设备信息 */
export const updateDevice = (
  deviceId: string,
  data: { name?: string; manufacturer?: string; model?: string; firmware?: string }
) => http.request<BaseResult<{ deviceId: string }>>("patch", baseUrlApi(`gb28181/device/${deviceId}`), { data });

// ===== SIP 平台接入信息 =====

export interface SipPlatformInfo {
  version: string;
  enabled: boolean;
  serverId: string;
  domain: string;
  sipIp: string;
  sipIps: string[];
  sipPort: number;
  transport: string[];
  passwordMasked: string;
  registerUri: string;
  listenIp: string;
  advertiseIp: string;
  deploymentMode?: SipDeploymentMode;
  configStatus: "configured" | "unconfigured";
  runtime: SipRuntimeStatus;
  restartRequired: boolean;
}

export const fetchSipPlatformInfo = () => http.request<BaseResult<SipPlatformInfo>>("get", baseUrlApi("gb28181/sip/platform"));

// ===== 国标服务配置 =====

export interface PositionHistoryConfig {
  enabled: boolean;
  retentionDays: number;
}

export const fetchPositionHistoryConfig = () =>
  http.request<BaseResult<PositionHistoryConfig>>("get", baseUrlApi("gb28181/sip/service-config/position-history"));

export const updatePositionHistoryConfig = (config: PositionHistoryConfig) =>
  http.request<BaseResult<PositionHistoryConfig>>("put", baseUrlApi("gb28181/sip/service-config/position-history"), {
    data: config
  });

export interface SDPExtensionConfig {
  enabled: boolean;
}

export const fetchSDPExtensionConfig = () =>
  http.request<BaseResult<SDPExtensionConfig>>("get", baseUrlApi("gb28181/sip/service-config/sdp-extension"));

export const updateSDPExtensionConfig = (enabled: boolean) =>
  http.request<BaseResult<SDPExtensionConfig>>("put", baseUrlApi("gb28181/sip/service-config/sdp-extension"), {
    data: { enabled }
  });

export interface SyncChannelsOnOnlineConfig {
  enabled: boolean;
}

export const fetchSyncChannelsOnOnlineConfig = () =>
  http.request<BaseResult<SyncChannelsOnOnlineConfig>>("get", baseUrlApi("gb28181/sip/service-config/sync-channels-on-online"));

export const updateSyncChannelsOnOnlineConfig = (enabled: boolean) =>
  http.request<BaseResult<SyncChannelsOnOnlineConfig>>("put", baseUrlApi("gb28181/sip/service-config/sync-channels-on-online"), {
    data: { enabled }
  });

export interface OnlineOnHeartbeatConfig {
  enabled: boolean;
}

export const fetchOnlineOnHeartbeatConfig = () =>
  http.request<BaseResult<OnlineOnHeartbeatConfig>>("get", baseUrlApi("gb28181/sip/service-config/online-on-heartbeat"));

export const updateOnlineOnHeartbeatConfig = (enabled: boolean) =>
  http.request<BaseResult<OnlineOnHeartbeatConfig>>("put", baseUrlApi("gb28181/sip/service-config/online-on-heartbeat"), {
    data: { enabled }
  });

export interface SaveAlarmMessagesConfig {
  enabled: boolean;
}

export const fetchSaveAlarmMessagesConfig = () =>
  http.request<BaseResult<SaveAlarmMessagesConfig>>("get", baseUrlApi("gb28181/sip/service-config/save-alarm-messages"));

export const updateSaveAlarmMessagesConfig = (enabled: boolean) =>
  http.request<BaseResult<SaveAlarmMessagesConfig>>("put", baseUrlApi("gb28181/sip/service-config/save-alarm-messages"), {
    data: { enabled }
  });

export interface SIPCommandTimeoutConfig {
  timeoutSec: number;
}

export const fetchSIPCommandTimeoutConfig = () =>
  http.request<BaseResult<SIPCommandTimeoutConfig>>("get", baseUrlApi("gb28181/sip/service-config/sip-command-timeout"));

export const updateSIPCommandTimeoutConfig = (timeoutSec: number) =>
  http.request<BaseResult<SIPCommandTimeoutConfig>>("put", baseUrlApi("gb28181/sip/service-config/sip-command-timeout"), {
    data: { timeoutSec }
  });

export interface PreallocationModeConfig {
  enabled: boolean;
}

export const fetchPreallocationModeConfig = () =>
  http.request<BaseResult<PreallocationModeConfig>>("get", baseUrlApi("gb28181/sip/service-config/preallocation-mode"));

export const updatePreallocationModeConfig = (enabled: boolean) =>
  http.request<BaseResult<PreallocationModeConfig>>("put", baseUrlApi("gb28181/sip/service-config/preallocation-mode"), {
    data: { enabled }
  });

export interface IgnoreChannelOfflineStatusNotifyConfig {
  enabled: boolean;
}

export const fetchIgnoreChannelOfflineStatusNotifyConfig = () =>
  http.request<BaseResult<IgnoreChannelOfflineStatusNotifyConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/ignore-channel-offline-status-notify")
  );

export const updateIgnoreChannelOfflineStatusNotifyConfig = (enabled: boolean) =>
  http.request<BaseResult<IgnoreChannelOfflineStatusNotifyConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/ignore-channel-offline-status-notify"),
    {
      data: { enabled }
    }
  );

export interface PTZDefaultSpeedConfig {
  level: number;
}

export const fetchPTZDefaultSpeedConfig = () =>
  http.request<BaseResult<PTZDefaultSpeedConfig>>("get", baseUrlApi("gb28181/sip/service-config/ptz-default-speed"));

export const updatePTZDefaultSpeedConfig = (level: number) =>
  http.request<BaseResult<PTZDefaultSpeedConfig>>("put", baseUrlApi("gb28181/sip/service-config/ptz-default-speed"), {
    data: { level }
  });

export type ChannelStreamTransport = "UDP" | "TCP-Active" | "TCP-Passive";
export type PlaybackProtocol = "ws-flv" | "http-flv" | "hls" | "webrtc";

export interface DefaultChannelStreamTransportConfig {
  transport: ChannelStreamTransport;
}

export const fetchDefaultChannelStreamTransportConfig = () =>
  http.request<BaseResult<DefaultChannelStreamTransportConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/default-channel-stream-transport")
  );

export const updateDefaultChannelStreamTransportConfig = (transport: ChannelStreamTransport) =>
  http.request<BaseResult<DefaultChannelStreamTransportConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/default-channel-stream-transport"),
    { data: { transport } }
  );

export interface DefaultPlaybackProtocolConfig {
  protocol: PlaybackProtocol;
}

export const fetchDefaultPlaybackProtocolConfig = () =>
  http.request<BaseResult<DefaultPlaybackProtocolConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/default-playback-protocol")
  );

export const updateDefaultPlaybackProtocolConfig = (protocol: PlaybackProtocol) =>
  http.request<BaseResult<DefaultPlaybackProtocolConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/default-playback-protocol"),
    { data: { protocol } }
  );

export interface PlaybackSettingsConfig {
  playTimeoutMs: number;
  onDemandLive: boolean;
  cloudRecordingEnabled: boolean;
}

export const fetchPlaybackSettingsConfig = () =>
  http.request<BaseResult<PlaybackSettingsConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/playback-settings")
  );

export const updatePlaybackSettingsConfig = (config: PlaybackSettingsConfig) =>
  http.request<BaseResult<PlaybackSettingsConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/playback-settings"),
    { data: config }
  );

export interface FixedAddressPlaybackConfig {
  fixedAddressEnabled: boolean;
  autoOnDemandEnabled: boolean;
}

export const fetchFixedAddressPlaybackConfig = () =>
  http.request<BaseResult<FixedAddressPlaybackConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/fixed-address-playback")
  );

export const updateFixedAddressPlaybackConfig = (config: FixedAddressPlaybackConfig) =>
  http.request<BaseResult<FixedAddressPlaybackConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/fixed-address-playback"),
    { data: config }
  );

export interface PlayAuthConfig {
  authEnabled: boolean;
  authBindClientIP: boolean;
  authTTLSeconds: number;
}

export const fetchPlayAuthConfig = () =>
  http.request<BaseResult<PlayAuthConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/play-auth")
  );

export const updatePlayAuthConfig = (config: PlayAuthConfig) =>
  http.request<BaseResult<PlayAuthConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/play-auth"),
    { data: config }
  );

export type GlobalSubscriptionItem = "catalog" | "mobile_position" | "alarm" | "ptz_precise_position";

export interface GlobalSubscriptionConfig {
  items: GlobalSubscriptionItem[];
}

export const fetchGlobalSubscriptionConfig = () =>
  http.request<BaseResult<GlobalSubscriptionConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/global-subscriptions")
  );

export const updateGlobalSubscriptionConfig = (items: GlobalSubscriptionItem[]) =>
  http.request<BaseResult<GlobalSubscriptionConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/global-subscriptions"),
    { data: { items } }
  );

export interface DefaultChannelAudioConfig {
  enabled: boolean;
}

export const fetchDefaultChannelAudioConfig = () =>
  http.request<BaseResult<DefaultChannelAudioConfig>>(
    "get",
    baseUrlApi("gb28181/sip/service-config/default-channel-audio")
  );

export const updateDefaultChannelAudioConfig = (enabled: boolean) =>
  http.request<BaseResult<DefaultChannelAudioConfig>>(
    "put",
    baseUrlApi("gb28181/sip/service-config/default-channel-audio"),
    { data: { enabled } }
  );

export interface SIPLogConfig {
  enabled: boolean;
  retentionDays?: number;
  applied: boolean;
  applyError?: string;
}

export const fetchSIPLogConfig = () =>
  http.request<BaseResult<SIPLogConfig>>("get", baseUrlApi("gb28181/sip/service-config/sip-log"));

export const updateSIPLogConfig = (config: { enabled: boolean; retentionDays: number }) =>
  http.request<BaseResult<SIPLogConfig>>("put", baseUrlApi("gb28181/sip/service-config/sip-log"), {
    data: config
  });

export type SipDeploymentMode = "lan" | "public";
// 2026-07-20 后端简化:runtime state 仍是六态,restart_required 语义已废弃(保留兼容枚举,新代码不产生).
export type SipRuntimeState = "disabled" | "unconfigured" | "starting" | "running" | "failed" | "restart_required";

export interface SipRuntimeStatus {
  state: SipRuntimeState;
  errorSummary?: string;
  updatedAt: string;
  startedAt?: string;
}

export interface SipConfigSummary {
  deploymentMode: SipDeploymentMode;
  listenIp: string;
  advertiseIp: string;
  advertiseIpInferred: boolean;
  port: number;
  domain: string;
  serverId: string;
  // 明文密码.已认证 + 有 SIP 配置权限的调用方才能拿到 —— 用户抄给设备用.
  password: string;
  hasPassword: boolean;
}

// SipSetupStatus 是 /api/gb28181/sip/setup/status 的响应.
// 2026-07-20 起字段精简 —— 只保留 configStatus + config + runtime,不再有 onboardingStatus 四态.
// 前端 gating 只看 runtime.state === "unconfigured".
export interface SipSetupStatus {
  configStatus: "configured" | "unconfigured";
  config?: SipConfigSummary;
  runtime: SipRuntimeStatus;
}

export interface SipNetworkAddress {
  ip: string;
  interfaceName?: string;
  cidr: string;
  loopback: boolean;
  virtual: boolean;
  recommended: boolean;
  more: boolean;
  listenOnly: boolean;
}

export interface SipNetworkInterfaces {
  items: SipNetworkAddress[];
  scanStatus: "ok" | "failed";
  warning?: string;
}

export interface SaveSipConfigPayload {
  deploymentMode: SipDeploymentMode;
  listenIp: string;
  advertiseIp: string;
  advertiseIpInferred: boolean;
  port: number;
  domain: string;
  serverId: string;
  password?: string;
}

export const fetchSipSetupStatus = () => http.request<BaseResult<SipSetupStatus>>("get", baseUrlApi("gb28181/sip/setup/status"));

export const fetchSipNetworkInterfaces = () =>
  http.request<BaseResult<SipNetworkInterfaces>>("get", baseUrlApi("gb28181/sip/setup/network-interfaces"));

// 保存后端会立即热启动 SIP,响应体带 reloadedOk 表示是否成功,失败时 reloadError 是原因字符串.
export const saveSipSetupConfig = (data: SaveSipConfigPayload) =>
  http.request<BaseResult<{ config: SipConfigSummary; reloadedOk: boolean; reloadError: string; runtime: SipRuntimeStatus }>>(
    "put",
    baseUrlApi("gb28181/sip/setup/config"),
    { data }
  );

// 2026-07-20 后端不再持久化 skip 状态,仅返回 acknowledged 用于审计. 暂缓由前端 sessionStorage 记住.
export const skipSipSetup = () =>
  http.request<BaseResult<{ acknowledged: boolean }>>("post", baseUrlApi("gb28181/sip/setup/skip"));

// ===== 扫码回填 SIP 接入信息 =====

export interface SipQrToken {
  token: string;
  // 相对秒数而非绝对时间戳:前端以响应到达时刻起算倒计时,免受客户端时钟偏移影响.
  expiresInSeconds: number;
}

/** 生成一次性接入 token(权限点 gb28181:sip:config:view) */
export const generateSipQrToken = () =>
  http.request<BaseResult<SipQrToken>>("post", baseUrlApi("gb28181/sip/qr/token"));

// ===== SIP 信令看板 =====

export const HEALTH_EMPTY = -1; // 后端 sentinel,前端识别后渲染 "--"

export interface TransactionStat {
  kind: string; // REGISTER / KEEPALIVE / CATALOG / INVITE / RECORD / ALARM / PTZ / BYE
  labelZh: string;
  labelEn: string;
  todayCount: number;
  successRate: number; // 0-1
  trendPct: number;
  alert: boolean;
}

export interface PulseSample {
  t: number; // unix 秒
  msgPerSec: number;
  failPct: number; // 千分位 (0-1000)
  known?: boolean; // false 表示该时间桶统计覆盖未知，不能当作 0
}

export interface AbnormalWindow {
  startT: number;
  endT: number;
}

export interface PulseData {
  windowMinutes: number;
  samples: PulseSample[];
  abnormalWindows: AbnormalWindow[];
}

export interface DashboardSnapshot {
  health: number; // -1 表示空数据
  todayTotal: number;
  todayAbnormal: number;
  pending: number;
  transactions: TransactionStat[];
  pulse: PulseData;
  partial?: boolean;
  asOf: number;
}

export type SnapshotResult = BaseResult<DashboardSnapshot>;

/** SIP 看板快照(REST 首屏) */
export const fetchSipDashboardSnapshot = (params: { window?: string; precision?: string } = {}) =>
  http.request<SnapshotResult>("get", baseUrlApi("gb28181/sip/dashboard/snapshot"), { params });

/** SIP 看板 SSE 流地址(EventSource 用)
 *
 * - URL 走 `/api/...` 相对路径让 vite proxy / nginx 同源代理,避开 CORS + EventSource 无法带 Authorization 头的限制
 * - 通过 `?token=xxx` 查询参数兜底鉴权(后端 `common.GetAccessToken` 已支持此通道)
 */
export const sipDashboardStreamUrl = (window = "60m", precision = "1m"): string => {
  const t = getAccessToken();
  const tokenPart = t?.accessToken ? `&token=${encodeURIComponent(t.accessToken)}` : "";
  return `/api/gb28181/sip/dashboard/stream?window=${window}&precision=${precision}${tokenPart}`;
};
