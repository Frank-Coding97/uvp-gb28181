import dayjs from "dayjs";

export type WorkbenchView = "text" | "session" | "matrix";
export type Direction = "inbound" | "outbound" | "";

export interface TraceFiltersState {
    view: WorkbenchView;
    range: [string, string];
    deviceId: string;
    direction: Direction;
    method: string;
    statusCode: string;
    callId: string;
    captureId: string;
    captureEndsAt: string;
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

    return {
        view,
        range,
        deviceId: strFrom("deviceId"),
        direction,
        method: strFrom("method"),
        statusCode: strFrom("statusCode"),
        callId: strFrom("callId"),
        captureId: strFrom("captureId"),
        captureEndsAt: strFrom("captureEndsAt")
    };
}

export function toQuery(state: TraceFiltersState): Record<string, string> {
    const q: Record<string, string> = { view: state.view };
    if (state.range[0]) q.from = state.range[0];
    if (state.range[1]) q.to = state.range[1];
    if (state.deviceId) q.deviceId = state.deviceId;
    if (state.direction) q.direction = state.direction;
    if (state.method) q.method = state.method;
    if (state.statusCode) q.statusCode = state.statusCode;
    if (state.callId) q.callId = state.callId;
    if (state.captureId) q.captureId = state.captureId;
    if (state.captureEndsAt) q.captureEndsAt = state.captureEndsAt;
    return q;
}
