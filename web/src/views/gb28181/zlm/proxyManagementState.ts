import type {
  ZLMCapabilityState,
  ZLMPullProxyCreateRequest,
  ZLMProxyDeletePreflight,
  ZLMURLSummary,
  ZLMPushProxyCreateRequest
} from "@/api/gb28181-zlm-ingress";

export type ProxyTab = "pull" | "push";

export interface ProxyFormState {
  schema: string;
  vhost: string;
  app: string;
  stream: string;
  url: string;
  retryCount: string;
  rtpType: string;
  timeoutSec: string;
}

export interface ProxyFormResult {
  errors: Record<string, string>;
  request?: ZLMPullProxyCreateRequest | ZLMPushProxyCreateRequest;
}

const pullSchemes = new Set(["rtsp", "rtsps", "rtmp", "rtmps", "http", "https"]);
const pushSchemes = new Set(["rtsp", "rtsps", "rtmp", "rtmps"]);

function optionalInteger(raw: string, min: number, max: number) {
  const value = raw.trim();
  if (!value) return { value: undefined };
  if (!/^\d+$/.test(value)) return { error: `请输入 ${min} 到 ${max} 的整数` };
  const number = Number(value);
  return Number.isSafeInteger(number) && number >= min && number <= max
    ? { value: number }
    : { error: `请输入 ${min} 到 ${max} 的整数` };
}

function optionalDecimal(raw: string, min: number, max: number) {
  const value = raw.trim();
  if (!value) return { value: undefined };
  if (!/^\d+(?:\.\d+)?$/.test(value)) return { error: `请输入 ${min} 到 ${max} 的数值` };
  const number = Number(value);
  if (!Number.isFinite(number) || number > max || (number !== 0 && number < min)) {
    return { error: `请输入 ${min} 到 ${max} 的数值，0 使用后端默认值` };
  }
  return { value: number };
}

function safeProxyURL(raw: string, schemes: Set<string>) {
  if (!raw || raw !== raw.trim() || /[\s\u0000-\u001f\u007f]/.test(raw)) return null;
  try {
    const parsed = new URL(raw);
    const scheme = parsed.protocol.slice(0, -1).toLowerCase();
    if (!schemes.has(scheme) || !parsed.hostname || parsed.hash) return null;
    const port = parsed.port ? Number(parsed.port) : 0;
    if (parsed.port && (!Number.isInteger(port) || port < 1 || port > 65535)) return null;
    return { scheme };
  } catch {
    return null;
  }
}

function sameProtocol(left: string, right: string) {
  const base = (value: string) => value.toLowerCase().replace(/s$/, "");
  return base(left) === base(right);
}

export function buildProxyCreateRequest(kind: ProxyTab, form: ProxyFormState): ProxyFormResult {
  const errors: Record<string, string> = {};
  const media = {
    schema: form.schema.trim().toLowerCase(),
    vhost: form.vhost.trim(),
    app: form.app.trim(),
    stream: form.stream.trim()
  };
  for (const [key, value] of Object.entries(media)) {
    if (!value) errors[key] = "不能为空";
  }

  const allowed = kind === "pull" ? pullSchemes : pushSchemes;
  const parsedURL = safeProxyURL(form.url, allowed);
  if (!parsedURL) errors.url = kind === "pull" ? "请输入受支持的拉流地址" : "请输入受支持的推流地址";
  if (kind === "push" && parsedURL && !sameProtocol(media.schema, parsedURL.scheme)) {
    errors.url = "目标地址协议必须与本地媒体 Schema 一致";
  }

  const retry = optionalInteger(form.retryCount, 0, 10);
  const rtpType = optionalInteger(form.rtpType, 0, 2);
  const timeout = optionalDecimal(form.timeoutSec, 0.1, 30);
  if (retry.error) errors.retryCount = retry.error;
  if (rtpType.error) errors.rtpType = rtpType.error;
  if (timeout.error) errors.timeoutSec = timeout.error;
  if (Object.keys(errors).length) return { errors };

  const options = {
    ...(retry.value !== undefined ? { retryCount: retry.value } : {}),
    ...(rtpType.value !== undefined ? { rtpType: rtpType.value } : {}),
    ...(timeout.value !== undefined ? { timeoutSec: timeout.value } : {})
  };
  return kind === "pull"
    ? { errors, request: { media, sourceUrl: form.url, ...options } }
    : { errors, request: { media, targetUrl: form.url, ...options } };
}

export function proxyCapabilityPresentation(state: ZLMCapabilityState) {
  if (state === "supported") return { actionable: true, label: "节点支持", tone: "success" as const };
  if (state === "unsupported") return { actionable: false, label: "节点不支持", tone: "danger" as const };
  return { actionable: false, label: "能力未探测", tone: "warning" as const };
}

export function ingressCapabilityFromError(error: unknown): ZLMCapabilityState {
  const candidate = error as { status?: number; response?: { status?: number } } | null;
  return (candidate?.response?.status ?? candidate?.status) === 422 ? "unsupported" : "unknown";
}

export function proxyAddressText(summary?: ZLMURLSummary) {
  return summary?.display?.trim() || "—";
}

export function proxyDeleteDecision(preflight: Pick<ZLMProxyDeletePreflight, "status" | "present" | "presenceKnown">) {
  if (!preflight.present && preflight.presenceKnown) {
    return { allowed: false, reason: "后端确认代理已不存在，无需再次删除。" };
  }
  if (preflight.status === "managed" && preflight.present && preflight.presenceKnown) {
    return { allowed: true, reason: "后端确认该代理由管理台创建且当前存在。" };
  }
  return { allowed: false, reason: "代理受业务持有或归属未知，普通删除已保护。" };
}
