import dayjs from "dayjs";
import type {
  TraceDiagnosisCode,
  TraceMessageSummary as TraceMessage,
  TraceSessionSummary as TraceSession
} from "@/api/gb28181-trace";

export function formatTime(value: string): string {
  return dayjs(value).format("HH:mm:ss.SSS");
}

export function formatFullTime(value: string): string {
  return dayjs(value).format("MM-DD HH:mm:ss.SSS");
}

// 报文时间戳是 RFC3339Nano（小数位数最多 9 位）。浏览器 Date 只有毫秒精度，
// 用它重排会话报文会把同毫秒内的先后抹平：列表接口按 occurred_at DESC 返回，
// 稳定排序保留倒序后，「401 应答」就排到了「REGISTER 请求」前面（时长达标但时序反了）。
// 这里按「整秒 + 小数部分补零到 9 位」比较，才能真正还原到达顺序。
const OCCURRED_AT_FRACTION_WIDTH = 9;

function occurredAtParts(value: string): [number, string] {
  // ⛔ 只剥掉小数部分，时区后缀（Z / +08:00）必须留着，
  // 否则 Date.parse 会按浏览器本地时区解释，跨时区时会整体偏移。
  const second = Date.parse(value.replace(/\.\d+/, ""));
  const fraction = (value.match(/\.(\d+)/)?.[1] ?? "").slice(0, OCCURRED_AT_FRACTION_WIDTH);
  return [Number.isNaN(second) ? 0 : second, fraction.padEnd(OCCURRED_AT_FRACTION_WIDTH, "0")];
}

/** 按 occurredAt 升序比较，保留到纳秒（同一毫秒内的请求/应答不会被抹平） */
export function compareOccurredAt(a: string, b: string): number {
  const [secondA, fractionA] = occurredAtParts(a);
  const [secondB, fractionB] = occurredAtParts(b);
  if (secondA !== secondB) return secondA - secondB;
  if (fractionA === fractionB) return 0;
  return fractionA < fractionB ? -1 : 1;
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  if (ms < 3_600_000) return `${(ms / 60_000).toFixed(1)} min`;
  return `${(ms / 3_600_000).toFixed(1)} h`;
}

export function methodLabel(message: TraceMessage): string {
  if (message.statusCode) return String(message.statusCode);
  return message.method || message.cseqMethod || "?";
}

export type StatusTone = "success" | "warning" | "danger" | "neutral" | "info" | "accent";

export function statusTone(code: number): StatusTone {
  if (!code) return "info";
  if (code >= 500) return "danger";
  if (code >= 400) return "warning";
  if (code >= 300) return "neutral";
  if (code >= 200) return "success";
  if (code >= 100) return "info";
  return "neutral";
}

export function methodTone(method: string): StatusTone {
  switch (method) {
    case "REGISTER":
    case "SUBSCRIBE":
    case "NOTIFY":
      return "info";
    case "INVITE":
    case "ACK":
      return "success";
    case "BYE":
    case "CANCEL":
      return "warning";
    case "MESSAGE":
      return "accent";
    default:
      return "neutral";
  }
}

export function methodChainTone(step: string): StatusTone {
  if (/^\d+$/.test(step)) return statusTone(Number(step));
  if (step === "...") return "neutral";
  return methodTone(step);
}

export function sessionStateLabel(session: TraceSession): { label: string; tone: StatusTone } {
  if (session.diagnosis) {
    return {
      label: diagnosisLabel(session.diagnosis.code),
      tone: session.diagnosis.category === "register_failure" ? "danger" : "warning"
    };
  }
  const methods = session.methods || [];
  const hasInvite = methods.includes("INVITE");
  const hasRegister = methods.includes("REGISTER");
  if (hasRegister && session.finalStatus === 401) return { label: "认证挑战", tone: "info" };
  if (hasInvite && session.finalStatus < 200) return { label: "进行中", tone: "info" };
  if (session.finalStatus >= 400) return { label: `异常 ${session.finalStatus}`, tone: "danger" };
  if (session.finalStatus >= 200 && session.finalStatus < 300) return { label: "完成", tone: "success" };
  if (session.finalStatus === 100) return { label: "进行中", tone: "info" };
  return { label: "未知", tone: "neutral" };
}

const DIAGNOSIS_LABELS: Record<TraceDiagnosisCode, string> = {
  digest_failure: "摘要认证失败",
  nonce_invalid: "Nonce 无效",
  nonce_expired: "Nonce 已过期",
  nonce_replay: "Nonce 重放",
  server_id_mismatch: "平台 ID 不匹配",
  device_not_preallocated: "设备未预分配",
  invalid_request: "注册请求无效",
  internal_error: "平台内部错误",
  timeout: "注册超时",
  undetermined: "注册失败（待判断）",
  signaling_timeout: "信令响应超时",
  media_timeout: "信令成功，媒体未就绪"
};

export function diagnosisLabel(code: TraceDiagnosisCode): string {
  return DIAGNOSIS_LABELS[code] || code;
}

function splitHeaderBody(payload: string): { header: string; body: string } {
  const marker = payload.indexOf("\r\n\r\n");
  if (marker < 0) return { header: payload, body: "" };
  return { header: payload.slice(0, marker), body: payload.slice(marker + 4) };
}

export interface ParsedPayload {
  startLine: string;
  headers: Array<{ name: string; value: string }>;
  body: string;
  bodyType: "sdp" | "xml" | "text" | "empty";
}

export function parsePayload(payload: string): ParsedPayload {
  const { header, body } = splitHeaderBody(payload);
  const lines = header.split("\r\n");
  const startLine = lines.shift() || "";
  const headers: Array<{ name: string; value: string }> = [];
  for (const line of lines) {
    if (!line.trim()) continue;
    const colon = line.indexOf(":");
    if (colon < 0) continue;
    headers.push({ name: line.slice(0, colon).trim(), value: line.slice(colon + 1).trim() });
  }
  const bodyTrimmed = body.trim();
  let bodyType: ParsedPayload["bodyType"] = "empty";
  if (bodyTrimmed.startsWith("v=0")) bodyType = "sdp";
  else if (bodyTrimmed.startsWith("<?xml") || bodyTrimmed.startsWith("<")) bodyType = "xml";
  else if (bodyTrimmed.length > 0) bodyType = "text";
  return { startLine, headers, body, bodyType };
}

// 关键 SIP header — 运维定位问题最先看的
export const KEY_HEADER_NAMES = new Set([
  "From",
  "To",
  "Call-ID",
  "CSeq",
  "Via",
  "Contact",
  "User-Agent",
  "Expires",
  "Event",
  "Subject",
  "Content-Type",
  "Content-Length",
  "WWW-Authenticate",
  "Authorization",
  "Max-Forwards"
]);

export function isKeyHeader(name: string): boolean {
  return KEY_HEADER_NAMES.has(name);
}

// ============ 语法高亮(sngrep 风格) ============

function escapeHtml(input: string): string {
  return input.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function highlightSipUri(escaped: string): string {
  // 匹配已 escape 后的 &lt;sip:...&gt; 形式
  return escaped.replace(/(&lt;sip:)([^&]+?)(&gt;)/gi, (_m, a, uri, c) => {
    return `${a}<span class="tok-uri">${uri}</span>${c}`;
  });
}

function highlightIpPort(escaped: string): string {
  // IPv4:port
  return escaped.replace(/\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})(:\d+)?\b/g, (_m, ip, port) => {
    return `<span class="tok-ip">${ip}</span>${port ? `<span class="tok-port">${port}</span>` : ""}`;
  });
}

function highlightSipStartLine(line: string): string {
  let out = escapeHtml(line);
  // Request: METHOD sip:xxx SIP/2.0
  const reqMatch = /^([A-Z]+) (.+) (SIP\/2\.0)$/.exec(line);
  if (reqMatch) {
    const escapedMethod = escapeHtml(reqMatch[1]);
    const escapedUri = escapeHtml(reqMatch[2]);
    const escapedVer = escapeHtml(reqMatch[3]);
    out = `<span class="tok-method">${escapedMethod}</span> <span class="tok-uri-line">${highlightIpPort(escapedUri)}</span> <span class="tok-version">${escapedVer}</span>`;
    return out;
  }
  // Response: SIP/2.0 200 OK
  const respMatch = /^(SIP\/2\.0) (\d{3}) (.+)$/.exec(line);
  if (respMatch) {
    const code = Number(respMatch[2]);
    let toneClass = "tok-status-info";
    if (code >= 200 && code < 300) toneClass = "tok-status-success";
    else if (code >= 400) toneClass = "tok-status-danger";
    else if (code >= 300) toneClass = "tok-status-warning";
    out = `<span class="tok-version">${escapeHtml(respMatch[1])}</span> <span class="${toneClass}">${respMatch[2]}</span> <span class="${toneClass}">${escapeHtml(respMatch[3])}</span>`;
    return out;
  }
  return out;
}

export function highlightSipHeaderValue(name: string, value: string): string {
  let escaped = escapeHtml(value);
  escaped = highlightSipUri(escaped);
  escaped = highlightIpPort(escaped);
  // Call-ID 值高亮
  if (name === "Call-ID") {
    return `<span class="tok-callid">${escapeHtml(value)}</span>`;
  }
  // CSeq: 数字 + 方法
  if (name === "CSeq") {
    const m = /^(\d+)\s+([A-Z]+)$/.exec(value);
    if (m) return `<span class="tok-number">${m[1]}</span> <span class="tok-method">${m[2]}</span>`;
  }
  // Content-Length: 数字
  if (name === "Content-Length" || name === "Max-Forwards" || name === "Expires") {
    return `<span class="tok-number">${escapeHtml(value)}</span>`;
  }
  return escaped;
}

// 完整 SIP payload 高亮(原文 tab 用)
export function highlightSipPayload(payload: string): string {
  const lines = payload.split(/\r?\n/);
  if (!lines.length) return escapeHtml(payload);
  const startLine = lines.shift() || "";
  const result: string[] = [];
  result.push(highlightSipStartLine(startLine));

  let inBody = false;
  for (const line of lines) {
    if (!inBody && line === "") {
      inBody = true;
      result.push("");
      continue;
    }
    if (inBody) {
      // 正文按内容类型识别
      if (line.trimStart().startsWith("<")) {
        result.push(highlightXml(line));
      } else if (/^[a-z]=/.test(line)) {
        result.push(highlightSdp(line));
      } else {
        result.push(escapeHtml(line));
      }
      continue;
    }
    // Header 行:Name: Value
    const colon = line.indexOf(":");
    if (colon > 0) {
      const name = line.slice(0, colon);
      const value = line.slice(colon + 1).trimStart();
      const gap = line.slice(colon + 1, colon + 1 + (line.length - (colon + 1) - value.length));
      result.push(
        `<span class="tok-header-key">${escapeHtml(name)}</span>:${escapeHtml(gap)}${highlightSipHeaderValue(name, value)}`
      );
    } else {
      result.push(escapeHtml(line));
    }
  }
  return result.join("\n");
}

export function highlightXml(text: string): string {
  let out = escapeHtml(text);
  // 标签名着色: &lt;tag ...&gt; 和 &lt;/tag&gt;
  out = out.replace(/(&lt;\/?)([a-zA-Z][\w-]*)/g, (_m, prefix, name) => {
    return `${prefix}<span class="tok-xml-tag">${name}</span>`;
  });
  // 属性名="值"
  out = out.replace(/([a-zA-Z_][\w-]*)=(&quot;)([^&]*?)(&quot;)/g, (_m, attr, q1, val, q2) => {
    return `<span class="tok-xml-attr">${attr}</span>=${q1}<span class="tok-xml-string">${val}</span>${q2}`;
  });
  return out;
}

export function highlightSdp(text: string): string {
  return text
    .split(/\r?\n/)
    .map(line => {
      const m = /^([a-z])=(.*)$/.exec(line);
      if (m) {
        return `<span class="tok-sdp-key">${m[1]}=</span>${highlightIpPort(escapeHtml(m[2]))}`;
      }
      return escapeHtml(line);
    })
    .join("\n");
}
