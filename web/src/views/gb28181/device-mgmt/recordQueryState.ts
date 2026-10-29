import dayjs from "dayjs";
import type { RecordQueryItem, RecordQueryOptions, RecordQueryRequest, RecordQueryRequestType } from "./api";

export type RecordQueryUiState = "idle" | "querying" | "complete" | "empty" | "partial" | "timeout" | "offline" | "error";

export type RecordQueryForm = RecordQueryRequest;

export interface RecordQueryValidationErrors {
  startTime?: string;
  endTime?: string;
  type?: string;
  secrecy?: string;
}

export function recordQueryOptionsPath(_channelId: number): string {
  return `gb28181/device-mgmt/channel/${_channelId}/record-query/options`;
}

export function recordQueryPath(_channelId: number): string {
  return `gb28181/device-mgmt/channel/${_channelId}/record-query`;
}

export function createDefaultRecordQueryForm(options: RecordQueryOptions): RecordQueryForm {
  const serverNow = dayjs(options.serverNow).tz(options.timezone);
  return {
    startTime: serverNow.startOf("day").format("YYYY-MM-DDTHH:mm:ss"),
    endTime: serverNow.format("YYYY-MM-DDTHH:mm:ss"),
    type: "all",
    secrecy: 0,
    recorderId: ""
  };
}

export function serializeRecordQueryForm(form: RecordQueryForm): RecordQueryRequest {
  return {
    startTime: form.startTime,
    endTime: form.endTime,
    type: form.type,
    secrecy: form.secrecy,
    recorderId: form.recorderId.trim()
  };
}

/**
 * 录像查询请求的额外超时余量。
 *
 * 后端聚合录像目录的时限由 `record_query.timeout_sec` 决定(交付包默认 15 秒),到点
 * 它会返回「部分结果」而不是失败。如果前端也卡在 15 秒(全局 axios 默认值),就会
 * 在后端写出响应之前十几毫秒先断开 —— 实测后端 15015ms、前端 15000ms,于是用户
 * 永远只看到「请求超时」,明明已经查到的录像段一条也拿不到。
 *
 * 所以前端必须比后端多留一截:既保证后端的结论一定送达,又不至于让用户白等。
 */
export const RECORD_QUERY_TIMEOUT_MARGIN_MS = 5000;

export const RECORD_QUERY_DEFAULT_TIMEOUT_SECONDS = 15;

export function recordQueryTimeoutMs(options?: Pick<RecordQueryOptions, "timeoutSeconds"> | null): number {
  const backendSeconds = Number(options?.timeoutSeconds);
  const effectiveSeconds =
    Number.isFinite(backendSeconds) && backendSeconds > 0 ? backendSeconds : RECORD_QUERY_DEFAULT_TIMEOUT_SECONDS;
  return effectiveSeconds * 1000 + RECORD_QUERY_TIMEOUT_MARGIN_MS;
}

export function validateRecordQueryForm(
  form: RecordQueryForm,
  options: Pick<RecordQueryOptions, "maxRangeHours" | "supportedTypes" | "timezone">
): RecordQueryValidationErrors {
  const errors: RecordQueryValidationErrors = {};
  const start = dayjs.tz(form.startTime, options.timezone);
  const end = dayjs.tz(form.endTime, options.timezone);
  if (!form.startTime || !start.isValid()) errors.startTime = "请选择有效的开始时间";
  if (!form.endTime || !end.isValid()) errors.endTime = "请选择有效的结束时间";
  if (!errors.startTime && !errors.endTime) {
    if (!end.isAfter(start)) errors.endTime = "结束时间必须晚于开始时间";
    // 不再按 maxRangeHours 拦截：跨度是用户的真实诉求，设备侧由条目上限与查询超时兜底。
    // 保留该字段只为展示「常规跨度」，超了照常查询。
  }
  if (!options.supportedTypes.includes(form.type)) errors.type = "请选择设备支持的录像类型";
  if (!Number.isInteger(form.secrecy) || form.secrecy < 0) errors.secrecy = "保密属性必须是非负整数";
  return errors;
}

// axios 自己生成的技术串：`Request failed with status code 404` / `Network Error` /
// `timeout of 15000ms exceeded`。这些串对用户没有任何信息量，一旦漏到界面上，
// 用户既不知道该找谁，售后也得反过来问一句「你截图里的英文是什么」。
const axiosEnglishPattern = /request failed with status code|network error|timeout of|\baborted\b/i;

/**
 * 把一次失败翻成用户能读懂的中文。
 *
 * 优先级：后端给的 message → 调用方给的业务兜底 → 按 HTTP 状态给通用中文。
 * 只有既不像 axios 技术串、又不是空白的 Error.message 才会原样透出。
 */
export function readableHttpError(cause: unknown, fallback: string): string {
  const candidate = cause as {
    code?: string;
    message?: string;
    response?: { status?: number; data?: { message?: string } };
  };
  const backendMessage = candidate?.response?.data?.message?.trim();
  if (backendMessage && !axiosEnglishPattern.test(backendMessage)) return backendMessage;
  const status = Number(candidate?.response?.status || 0);
  if (status === 401) return "登录状态已失效，请重新登录";
  if (status === 403) return "没有权限执行该操作";
  if (status === 404) return "通道不存在或无权访问";
  if (status === 409) return "通道正被占用，请稍后重试";
  if (status === 429) return "操作过于频繁，请稍后重试";
  if (status >= 500) return "服务暂不可用，请稍后重试";
  if (candidate?.code === "ECONNABORTED" || /timeout of/i.test(candidate?.message || "")) {
    return "请求超时，请检查网络后重试";
  }
  if (/network error/i.test(candidate?.message || "")) return "网络连接异常，请检查网络后重试";
  const raw = candidate?.message?.trim();
  if (raw && !axiosEnglishPattern.test(raw)) return raw;
  return fallback;
}

export function mapRecordQueryError(error: unknown): { state: RecordQueryUiState; message: string } {
  const candidate = error as {
    errorCode?: string;
    message?: string;
    response?: { data?: { message?: string; data?: { errorCode?: string } } };
  };
  const errorCode = candidate?.errorCode || candidate?.response?.data?.data?.errorCode;
  const state: RecordQueryUiState =
    errorCode === "record_query_device_offline" ? "offline" : errorCode === "record_query_timeout" ? "timeout" : "error";
  const fallbacks: Record<string, string> = {
    record_query_target_not_found: "通道不存在或无权访问",
    record_query_device_offline: "设备当前离线，无法查询录像",
    record_query_invalid_argument: "查询条件不符合要求",
    record_query_busy: "查询任务较多，请稍后重试",
    record_query_send_failed: "录像查询指令发送失败",
    record_query_unavailable: "录像查询服务暂不可用",
    record_query_timeout: "设备未返回录像目录"
  };
  const backendMessage = candidate?.response?.data?.message?.trim() || "";
  const readableBackendMessage = backendMessage && !axiosEnglishPattern.test(backendMessage) ? backendMessage : "";
  return {
    state,
    // 专属错误码的中文说明比通用映射更准确，优先用它；最后才按 HTTP 状态给通用文案。
    // ⛔ 任何分支都不允许把 candidate.message（axios 英文串）原样透出。
    message: readableBackendMessage || fallbacks[errorCode || ""] || readableHttpError(error, "查询失败，请稍后重试")
  };
}

export function recordQueryTypeText(type: string | null): string {
  return ({ time: "定时录像", alarm: "报警录像", manual: "手动录像" } as Record<string, string>)[type || ""] || "未知类型";
}

export function sortRecordQueryItems(items: RecordQueryItem[]): RecordQueryItem[] {
  return items
    .map((item, arrival) => ({ item, arrival }))
    .sort((left, right) => {
      const leftTime = left.item.startTime ? Date.parse(left.item.startTime) : Number.NaN;
      const rightTime = right.item.startTime ? Date.parse(right.item.startTime) : Number.NaN;
      const leftKnown = Number.isFinite(leftTime);
      const rightKnown = Number.isFinite(rightTime);
      if (leftKnown && rightKnown && leftTime !== rightTime) return leftTime - rightTime;
      if (leftKnown !== rightKnown) return leftKnown ? -1 : 1;
      return left.arrival - right.arrival;
    })
    .map(entry => entry.item);
}

export function paginateRecordQueryItems(items: RecordQueryItem[], page: number, pageSize: number): RecordQueryItem[] {
  const start = Math.max(0, page - 1) * pageSize;
  return items.slice(start, start + pageSize);
}

export const recordQueryRequestTypes: Array<{ label: string; value: RecordQueryRequestType }> = [
  { label: "全部录像", value: "all" },
  { label: "手动录像", value: "manual" },
  { label: "报警录像", value: "alarm" }
];
