import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import { BaseResult } from "./types";
import { getAccessToken } from "@/utils/auth";

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
    hls?: string | null;
    webrtc?: string | null;
    rtmp?: string | null;
    rtsp?: string | null;
  };
  urlWarnings?: string[];
  wsflvUrl: string;
  httpFlvUrl: string;
  hlsUrl: string;
  expireAt: number;
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
export const startPlay = (deviceId: string, channelId: string) =>
  http.request<PlayApiResult>("post", baseUrlApi(`gb28181/play/${deviceId}/${channelId}`));

/** 停播 */
export const stopPlay = (streamId: string) => http.request<BaseResult<unknown>>("delete", baseUrlApi(`gb28181/play/${streamId}`));

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
  http.request<BaseResult<StreamMonitorSnapshot>>("get", baseUrlApi(`gb28181/play/${streamId}/monitor`));

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
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/control-capabilities`)
  );

export interface DeviceOperationResult {
  operationId?: string;
  channelId?: string;
  action: string;
  id?: number;
  sn?: number;
  status?: string;
}

export const controlDevice = (channelId: number, data: Record<string, unknown>) =>
  http.request<BaseResult<DeviceOperationResult>>("post", baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/device-control`), {
    data
  });

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
  data: { action: "start" | "pause" | "resume" | "stop" | "delete"; trackId: number; idempotencyKey?: string }
) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/cruise`),
    { data }
  );

export const controlPtzAux = (
  channelId: number,
  data: { action: "on" | "off"; auxiliaryId: number; idempotencyKey?: string }
) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/aux`),
    { data }
  );

export const listPtzPresets = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ list: Array<Record<string, unknown>>; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/presets`),
    { params: refresh ? { refresh: true } : undefined }
  );

export const listCruiseTracks = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ list: Array<Record<string, unknown>>; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/cruise-tracks`),
    { params: refresh ? { refresh: true } : undefined }
  );

export const getHomePosition = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ homePosition: Record<string, unknown> | null; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/home-position`),
    { params: refresh ? { refresh: true } : undefined }
  );

export const updateHomePosition = (channelId: number, data: { enabled: boolean; resetTime: number; presetId: number }) =>
  http.request<BaseResult<DeviceOperationResult>>(
    "patch",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/home-position`),
    { data }
  );

export const getPtzPreciseStatus = (channelId: number, refresh = false) =>
  http.request<BaseResult<{ state: Record<string, unknown> | null; freshness: string }>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/ptz/precise-status`),
    { params: refresh ? { refresh: true } : undefined }
  );

export interface TalkCreateResult {
  sessionId: string;
  mode?: "broadcast" | "talk";
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
  mode?: "broadcast" | "talk";
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
  enabled: boolean;
  serverId: string;
  domain: string;
  sipIp: string;
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

export type SipDeploymentMode = "lan" | "public";
// 2026-07-20 后端简化:runtime state 仍是六态,restart_required 语义已废弃(保留兼容枚举,新代码不产生).
export type SipRuntimeState = "disabled" | "unconfigured" | "starting" | "running" | "failed" | "restart_required";

export interface SipRuntimeStatus {
  state: SipRuntimeState;
  errorSummary?: string;
  updatedAt: string;
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
