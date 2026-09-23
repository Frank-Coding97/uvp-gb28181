export type ZLMPresentationTone = "success" | "warning" | "danger" | "neutral";

export interface ZLMFreshnessPresentation {
  label: string;
  tone: ZLMPresentationTone;
  description: string;
}

export interface ZLMErrorPresentation {
  label: string;
  retryable: boolean;
}

function cleanNumber(value: unknown) {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function rounded(value: number) {
  if (value >= 100 || Number.isInteger(value)) return String(Math.round(value));
  return value.toFixed(1).replace(/\.0$/, "");
}

export function formatZLMBytes(value: unknown) {
  let current = cleanNumber(value);
  if (current === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let unit = 0;
  while (current >= 1024 && unit < units.length - 1) {
    current /= 1024;
    unit += 1;
  }
  return `${rounded(current)} ${units[unit]}`;
}

export function formatZLMByteRate(value: unknown) {
  return `${formatZLMBytes(value)}/s`;
}

export function formatZLMDuration(value: unknown) {
  let seconds = Math.floor(cleanNumber(value));
  if (seconds === 0) return "0秒";
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  seconds %= 60;
  const parts: string[] = [];
  if (hours > 0) parts.push(`${hours}小时`);
  if (minutes > 0) parts.push(`${minutes}分`);
  if (seconds > 0 || parts.length === 0) parts.push(`${seconds}秒`);
  return parts.join(" ");
}

const protocolLabels: Record<string, string> = {
  "http-flv": "HTTP-FLV",
  flv: "HTTP-FLV",
  hls: "HLS",
  http: "HTTP",
  https: "HTTPS",
  rtmp: "RTMP",
  rtmps: "RTMPS",
  rtsp: "RTSP",
  rtsps: "RTSPS",
  ts: "TS",
  webrtc: "WebRTC",
  gb28181: "GB28181"
};

export function formatZLMProtocol(value: unknown) {
  const normalized = String(value ?? "").trim().toLowerCase();
  if (!normalized) return "未知协议";
  return protocolLabels[normalized] ?? normalized.toUpperCase();
}

export function zlmFreshnessPresentation(value: string | number | Date | null | undefined, now = Date.now()): ZLMFreshnessPresentation {
  const sampledAt = value instanceof Date ? value.getTime() : typeof value === "number" ? value : Date.parse(value ?? "");
  if (!Number.isFinite(sampledAt)) {
    return { label: "暂无采样", tone: "neutral", description: "尚未收到可用的运行态采样" };
  }
  const seconds = Math.max(0, Math.floor((now - sampledAt) / 1000));
  if (seconds <= 15) return { label: "实时", tone: "success", description: `最近 ${seconds} 秒内已更新` };
  if (seconds <= 60) return { label: `延迟 ${seconds} 秒`, tone: "warning", description: "采样仍可参考，但更新已有延迟" };
  return { label: `已延迟 ${formatZLMDuration(seconds)}`, tone: "danger", description: "采样已过期，请刷新或检查节点状态" };
}

function errorStatus(error: unknown) {
  const candidate = error as { status?: number; response?: { status?: number; data?: { code?: string } }; code?: string } | null;
  return candidate?.response?.status ?? candidate?.status;
}

export function zlmErrorPresentation(error: unknown): ZLMErrorPresentation {
  switch (errorStatus(error)) {
    case 400:
      return { label: "请求参数无效", retryable: false };
    case 401:
      return { label: "登录状态已失效", retryable: false };
    case 403:
      return { label: "没有执行该操作的权限", retryable: false };
    case 404:
      return { label: "节点或媒体目标已不存在", retryable: false };
    case 409:
      return { label: "目标状态已变化，请重新确认", retryable: true };
    case 422:
      return { label: "当前节点不支持此能力", retryable: false };
    case 429:
      return { label: "请求过于频繁，请稍后重试", retryable: true };
    case 502:
    case 503:
      return { label: "媒体节点暂不可用", retryable: true };
    case 504:
      return { label: "媒体节点响应超时", retryable: true };
    default:
      return { label: "请求失败，请稍后重试", retryable: true };
  }
}
