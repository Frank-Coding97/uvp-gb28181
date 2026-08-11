import type { CascadeHeartbeatState, CascadePlatform, CascadeRegistrationState } from "@/api/gb28181";

export interface CascadePresentation { label: string; color: "green" | "red" | "orange" | "blue" | "gray"; detail: string }

export function cascadePresentation(platform: Pick<CascadePlatform, "enabled" | "overall" | "registration" | "heartbeat">): CascadePresentation {
  if (!platform.enabled) return { label: "已停用", color: "gray", detail: "平台未启用" };
  if (platform.overall === "online") return { label: "在线", color: "green", detail: "注册和心跳正常" };
  if (platform.registration === "expired") return { label: "注册已过期", color: "red", detail: "上级平台未保持注册" };
  if (platform.heartbeat === "stale") return { label: "心跳超时", color: "orange", detail: "最近心跳超过容忍窗口" };
  if (platform.registration === "registered") return { label: "等待心跳", color: "blue", detail: "已注册,等待有效心跳" };
  return { label: "等待注册", color: "blue", detail: "尚未收到上级注册确认" };
}

export function registrationLabel(value: CascadeRegistrationState): string {
  return ({ unregistered: "未注册", registered: "已注册", expired: "已过期" } as Record<string, string>)[value] || value || "未知";
}

export function heartbeatLabel(value: CascadeHeartbeatState): string {
  return ({ unknown: "未知", healthy: "正常", stale: "超时" } as Record<string, string>)[value] || value || "未知";
}

export function validGbId(value: string): boolean { return /^\d{20}$/.test(value.trim()); }

export function validPort(value: number): boolean { return Number.isInteger(value) && value >= 1 && value <= 65535; }

export function validHost(value: string): boolean {
  const host = value.trim();
  if (!host || host.length > 255 || /[\s/\\]/.test(host)) return false;
  return /^[a-zA-Z0-9.:[\]-]+$/.test(host);
}

export function validateCascadePlatform(form: { name: string; upstreamServerId: string; upstreamDomain: string; host: string; port: number; localDeviceId: string; localDomain: string; localSipIp: string; localSipPort: number; mediaAdvertiseIp?: string }): string[] {
  const errors: string[] = [];
  if (!form.name.trim()) errors.push("平台名称不能为空");
  if (!validGbId(form.upstreamServerId)) errors.push("上级平台 ID 必须是 20 位数字");
  if (!form.upstreamDomain.trim()) errors.push("上级域不能为空");
  if (!validHost(form.host)) errors.push("上级地址格式不正确");
  if (!validPort(form.port)) errors.push("上级端口必须在 1-65535 之间");
  if (!validGbId(form.localDeviceId)) errors.push("本平台设备 ID 必须是 20 位数字");
  if (!form.localDomain.trim()) errors.push("本平台域不能为空");
  if (!validHost(form.localSipIp)) errors.push("本地 SIP 地址格式不正确");
  if (!validPort(form.localSipPort)) errors.push("本地 SIP 端口必须在 1-65535 之间");
  if (form.mediaAdvertiseIp && !validHost(form.mediaAdvertiseIp)) errors.push("媒体宣告地址格式不正确");
  return errors;
}

export function defaultCascadePlatform() {
  return {
    name: "",
    upstreamServerId: "",
    upstreamDomain: "",
    host: "",
    port: 5060,
    localDeviceId: "",
    localDomain: "",
    localSipIp: "",
    localSipPort: 5060,
    mediaAdvertiseIp: "",
    authUsername: "",
    password: "",
    profileOverride: "auto",
    charsetOverride: "",
    registerExpires: 3600,
    keepaliveInterval: 60,
    retryPolicy: "",
    transport: "UDP",
    catalogBatchSize: 100,
    publishPlatform: false,
    publishCivil: false,
    publishGroup: false,
    maxStreams: 1,
    ptzEnabled: false,
    enabled: false
  };
}
