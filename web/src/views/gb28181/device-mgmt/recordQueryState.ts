import dayjs from "dayjs";
import type {
    RecordQueryItem,
    RecordQueryOptions,
    RecordQueryRequest,
    RecordQueryRequestType
} from "./api";

export type RecordQueryUiState = "idle" | "querying" | "complete" | "empty" | "partial" | "timeout" | "offline" | "error";

export interface RecordQueryForm extends RecordQueryRequest {}

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
        else if (end.diff(start, "hour", true) > options.maxRangeHours) {
            errors.endTime = `单次查询不能超过 ${options.maxRangeHours} 小时`;
        }
    }
    if (!options.supportedTypes.includes(form.type)) errors.type = "请选择设备支持的录像类型";
    if (!Number.isInteger(form.secrecy) || form.secrecy < 0) errors.secrecy = "保密属性必须是非负整数";
    return errors;
}

export function mapRecordQueryError(error: unknown): { state: RecordQueryUiState; message: string } {
    const candidate = error as {
        errorCode?: string;
        message?: string;
        response?: { data?: { message?: string; data?: { errorCode?: string } } };
    };
    const errorCode = candidate?.errorCode || candidate?.response?.data?.data?.errorCode;
    const state: RecordQueryUiState = errorCode === "record_query_device_offline"
        ? "offline"
        : errorCode === "record_query_timeout"
            ? "timeout"
            : "error";
    const fallbacks: Record<string, string> = {
        record_query_target_not_found: "通道不存在或无权访问",
        record_query_device_offline: "设备当前离线，无法查询录像",
        record_query_invalid_argument: "查询条件不符合要求",
        record_query_busy: "查询任务较多，请稍后重试",
        record_query_send_failed: "录像查询指令发送失败",
        record_query_unavailable: "录像查询服务暂不可用",
        record_query_timeout: "设备未返回录像目录"
    };
    return {
        state,
        message: candidate?.response?.data?.message || candidate?.message || fallbacks[errorCode || ""] || "查询失败，请稍后重试"
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
