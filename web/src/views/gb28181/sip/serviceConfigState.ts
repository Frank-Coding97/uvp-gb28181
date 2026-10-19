import type { CascadeSettingsConfig } from "@/api/gb28181";

export interface StaticServiceConfigDraft {
  defaultChannelStreamTransport: "UDP" | "TCP-Active" | "TCP-Passive";
  globalSubscriptionItems: Array<"catalog" | "mobile_position" | "alarm" | "ptz_precise_position">;
  defaultChannelAudioEnabled: boolean;
  saveMobilePositionHistory: boolean;
  positionHistoryRetentionDays: number;
  sdpExtension: boolean;
  ptzSpeed: number;
  syncChannelsOnOnline: boolean;
  sipLogEnabled: boolean;
  sipLogRetentionDays: number;
  cloudRecordingRetentionDays: number;
  ignoreChannelOfflineStatusNotify: boolean;
  onlineOnHeartbeat: boolean;
  saveAlarmMessages: boolean;
  sipTimeoutSec: number;
  preallocationMode: boolean;
  recordCacheTailToleranceSec: number;
  playback: {
    defaultProtocol: "ws-flv" | "http-flv" | "hls" | "webrtc";
    fixedAddressEnabled: boolean;
    autoOnDemandEnabled: boolean;
    authEnabled: boolean;
    authBindClientIP: boolean;
    authTTLSeconds: number;
    playTimeoutMs: number;
    cloudRecordingEnabled: boolean;
    onDemandLive: boolean;
  };
  cascade: CascadeSettingsConfig;
}

export const staticServiceConfigLabels = [
  "新通道默认流传输模式",
  "全局订阅项目",
  "全局通道开启音频",
  "保存移动位置历史轨迹",
  "位置历史保留天数（天）",
  "扩展 SDP 兼容模式",
  "云台默认速度",
  "设备上线时同步通道",
  "是否开启SIP日志",
  "云端录像默认保留天数（天）",
  "忽略通道离线/异常通知",
  "心跳恢复设备在线状态",
  "是否存储报警消息",
  "SIP 命令超时时间（秒）",
  "预分配模式",
  "录像缓存尾部容差（秒）"
] as const;

export const playbackServiceConfigLabels = [
  "默认播放协议",
  "固定播放地址",
  "自动点播",
  "播放鉴权",
  "绑定客户端 IP",
  "凭证有效期（秒）",
  "点播超时时间（毫秒）",
  "云端录像",
  "按需直播"
] as const;

export const cascadeServiceConfigLabels = ["User-Agent", "平台名称", "厂商", "型号", "行政区划", "安装地址"] as const;

export function createCascadeSettingsDraft(): CascadeSettingsConfig {
  return {
    userAgent: "UVP-GB28181",
    platformName: "UVP GB28181",
    manufacturer: "UVP",
    model: "UVP-GB28181",
    civilCode: "",
    address: ""
  };
}

export function validateCascadeSettings(settings: CascadeSettingsConfig): Partial<Record<keyof CascadeSettingsConfig, string>> {
  const errors: Partial<Record<keyof CascadeSettingsConfig, string>> = {};
  const fields = ["userAgent", "platformName", "manufacturer", "model", "address"] as const;
  for (const field of fields) {
    const value = settings[field];
    if (field !== "address" && !value.trim()) errors[field] = "不能为空";
    const max = field === "address" ? 256 : 128;
    if ([...value].length > max) errors[field] = `最多 ${max} 个字符`;
    if (/[\u0000-\u001f\u007f-\u009f]/.test(value)) errors[field] = "不能包含控制字符";
  }
  if (settings.civilCode && !/^(?:\d{2}|\d{4}|\d{6}|\d{8})$/.test(settings.civilCode)) {
    errors.civilCode = "请输入 2、4、6 或 8 位数字";
  }
  return errors;
}

export function createStaticServiceConfigDraft(): StaticServiceConfigDraft {
  return {
    defaultChannelStreamTransport: "TCP-Passive",
    globalSubscriptionItems: [],
    defaultChannelAudioEnabled: true,
    saveMobilePositionHistory: true,
    positionHistoryRetentionDays: 7,
    sdpExtension: false,
    ptzSpeed: 6,
    syncChannelsOnOnline: true,
    sipLogEnabled: false,
    sipLogRetentionDays: 7,
    cloudRecordingRetentionDays: 7,
    ignoreChannelOfflineStatusNotify: false,
    onlineOnHeartbeat: true,
    saveAlarmMessages: true,
    sipTimeoutSec: 10,
    preallocationMode: false,
    recordCacheTailToleranceSec: 2,
    playback: {
      defaultProtocol: "ws-flv",
      fixedAddressEnabled: false,
      autoOnDemandEnabled: false,
      authEnabled: false,
      authBindClientIP: false,
      authTTLSeconds: 120,
      playTimeoutMs: 10000,
      cloudRecordingEnabled: false,
      onDemandLive: true
    },
    cascade: createCascadeSettingsDraft()
  };
}

export interface PlayAuthDraft {
  authEnabled: boolean;
  authBindClientIP: boolean;
  authTTLSeconds?: number;
}

export function normalizePlayAuthConfig(config: PlayAuthDraft): Required<PlayAuthDraft> {
  return {
    authEnabled: config.authEnabled,
    authBindClientIP: config.authEnabled && config.authBindClientIP,
    authTTLSeconds: Number.isInteger(config.authTTLSeconds) ? config.authTTLSeconds! : 120
  };
}
