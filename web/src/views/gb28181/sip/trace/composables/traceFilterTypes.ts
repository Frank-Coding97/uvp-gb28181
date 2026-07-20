import dayjs from "dayjs";

export type WorkbenchView = "text" | "session" | "matrix";
export type Direction = "inbound" | "outbound" | "";

export interface TraceFiltersState {
    view: WorkbenchView;
    range: [string, string];
    deviceId: string;
    deviceIds: string[];
    direction: Direction;
    method: string;
    statusCode: string;
    statusCodeRange: string;
    callId: string;
    captureId: string;
    captureEndsAt: string;
    searchKeyword: string;
    anomalyOnly: boolean;
}

export interface TimeRange {
    start: number;
    end: number;
}

export function parseTimeRange(range: [string, string]): TimeRange | undefined {
    if (!range[0] || !range[1]) return undefined;
    const start = dayjs(range[0]).valueOf();
    const end = dayjs(range[1]).valueOf();
    return { start, end };
}

export const DEFAULT_RANGE_MINUTES = 15;

const DATE_FMT = "YYYY-MM-DD HH:mm:ss";

export function defaultRange(now = dayjs()): [string, string] {
    return [now.subtract(DEFAULT_RANGE_MINUTES, "minute").format(DATE_FMT), now.format(DATE_FMT)];
}

export function isView(value: unknown): value is WorkbenchView {
    return value === "text" || value === "session" || value === "matrix";
}

export function isDirection(value: unknown): value is Direction {
    return value === "inbound" || value === "outbound" || value === "";
}

export function parseQuery(query: Record<string, unknown>, now = dayjs()): TraceFiltersState {
    const rawView = query.view;
    const view: WorkbenchView = isView(rawView) ? rawView : "text";

    const from = typeof query.from === "string" ? query.from : "";
    const to = typeof query.to === "string" ? query.to : "";
    let range: [string, string];
    if (from && to) {
        range = [from, to];
    } else {
        range = defaultRange(now);
    }

    const rawDirection = query.direction;
    const direction: Direction = isDirection(rawDirection) ? rawDirection : "";

    const strFrom = (key: string) => (typeof query[key] === "string" ? (query[key] as string) : "");

    // Parse deviceIds from comma-separated string
    const deviceIdsStr = strFrom("deviceIds");
    const deviceIds = deviceIdsStr ? deviceIdsStr.split(",").filter(Boolean) : [];

    // Parse anomalyOnly boolean
    const anomalyOnly = query.anomalyOnly === "true" || query.anomalyOnly === true;

    return {
        view,
        range,
        deviceId: strFrom("deviceId"),
        deviceIds,
        direction,
        method: strFrom("method"),
        statusCode: strFrom("statusCode"),
        statusCodeRange: strFrom("statusCodeRange"),
        callId: strFrom("callId"),
        captureId: strFrom("captureId"),
        captureEndsAt: strFrom("captureEndsAt"),
        searchKeyword: strFrom("searchKeyword"),
        anomalyOnly
    };
}

export function toQuery(state: TraceFiltersState): Record<string, string> {
    const q: Record<string, string> = { view: state.view };
    if (state.range[0]) q.from = state.range[0];
    if (state.range[1]) q.to = state.range[1];
    if (state.deviceId) q.deviceId = state.deviceId;
    if (state.deviceIds && state.deviceIds.length > 0) q.deviceIds = state.deviceIds.join(",");
    if (state.direction) q.direction = state.direction;
    if (state.method) q.method = state.method;
    if (state.statusCode) q.statusCode = state.statusCode;
    if (state.statusCodeRange) q.statusCodeRange = state.statusCodeRange;
    if (state.callId) q.callId = state.callId;
    if (state.captureId) q.captureId = state.captureId;
    if (state.captureEndsAt) q.captureEndsAt = state.captureEndsAt;
    if (state.searchKeyword) q.searchKeyword = state.searchKeyword;
    if (state.anomalyOnly) q.anomalyOnly = "true";
    return q;
}
