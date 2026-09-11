import type { CascadePlatform, SipConfigSummary } from "@/api/gb28181";

export interface CascadePresentation { label: string; color: "green" | "red" | "orange" | "blue" | "gray"; detail: string }

export function cascadePresentation(platform: Pick<CascadePlatform, "enabled" | "overall" | "registration" | "heartbeat">): CascadePresentation {
  if (!platform.enabled) return { label: "已停用", color: "gray", detail: "平台未启用" };
  if (platform.overall === "online") return { label: "在线", color: "green", detail: "注册和心跳正常" };
  if (platform.registration === "expired") return { label: "注册已过期", color: "red", detail: "上级平台未保持注册" };
  if (platform.heartbeat === "stale") return { label: "心跳超时", color: "orange", detail: "最近心跳超过容忍窗口" };
  if (platform.registration === "registered") return { label: "等待心跳", color: "blue", detail: "已注册,等待有效心跳" };
  return { label: "等待注册", color: "blue", detail: "尚未收到上级注册确认" };
}

/** 注册/心跳周期展示文本（纯秒数，顺序同表头：注册 / 心跳）；0 视为未配置，按后端默认值（3600s/60s）展示。 */
export function cascadeCycleLabel(platform: Pick<CascadePlatform, "registerExpires" | "keepaliveInterval">): string {
  const expires = platform.registerExpires > 0 ? platform.registerExpires : 3600;
  const keepalive = platform.keepaliveInterval > 0 ? platform.keepaliveInterval : 60;
  return `${expires} / ${keepalive}`;
}

export function validGbId(value: string): boolean { return /^\d{20}$/.test(value.trim()); }

export function resolveChannelPTZAllowed(existing: boolean | undefined, platformEnabled: boolean): boolean {
  return existing === undefined ? platformEnabled : existing;
}

/**
 * Keeps a preferred GB identity when it is valid and unused, otherwise creates
 * a deterministic 20-digit local projection identity from the source row id.
 */
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

/** 编辑弹窗的逐字段错误(硬规则 4:blur 后才显示,提交时全量兜底)。返回的 key 与 form 字段同名。 */
export function cascadeFormFieldErrors(form: { name: string; upstreamServerId: string; upstreamDomain: string; host: string; localDeviceId: string; localDomain: string; localSipIp: string; mediaAdvertiseIp?: string }, touched: Record<string, boolean>): Record<string, string> {
  const errorOf = (field: string, message: string) => (touched[field] && message) || "";
  return {
    name: errorOf("name", form.name.trim() ? "" : "平台名称不能为空"),
    upstreamServerId: errorOf("upstreamServerId", !form.upstreamServerId.trim() ? "上级平台 ID 不能为空" : (validGbId(form.upstreamServerId) ? "" : "必须是 20 位数字编码")),
    upstreamDomain: errorOf("upstreamDomain", form.upstreamDomain.trim() ? "" : "上级域不能为空"),
    host: errorOf("host", !form.host.trim() ? "上级地址不能为空" : (validHost(form.host) ? "" : "地址格式不正确")),
    localDeviceId: errorOf("localDeviceId", !form.localDeviceId.trim() ? "本平台设备 ID 不能为空" : (validGbId(form.localDeviceId) ? "" : "必须是 20 位数字编码")),
    localDomain: errorOf("localDomain", form.localDomain.trim() ? "" : "本平台域不能为空"),
    localSipIp: errorOf("localSipIp", form.localSipIp.trim() ? (validHost(form.localSipIp) ? "" : "地址格式不正确") : "本地 SIP 地址不能为空"),
    mediaAdvertiseIp: errorOf("mediaAdvertiseIp", !form.mediaAdvertiseIp || validHost(form.mediaAdvertiseIp) ? "" : "地址格式不正确")
  };
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
    charsetOverride: "GB2312",
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

export function cascadeLocalIdentityDefaults(config?: Pick<SipConfigSummary, "listenIp" | "advertiseIp" | "port" | "domain" | "serverId">) {
  if (!config) return {};
  const localSipIp = config.advertiseIp || (config.listenIp !== "0.0.0.0" ? config.listenIp : "");
  return {
    localDeviceId: config.serverId,
    localDomain: config.domain,
    localSipIp,
    localSipPort: config.port,
    mediaAdvertiseIp: localSipIp,
    // GB/T 28181 认证用户名惯例上与本平台设备编码一致，给默认值省得手填
    authUsername: config.serverId
  };
}
