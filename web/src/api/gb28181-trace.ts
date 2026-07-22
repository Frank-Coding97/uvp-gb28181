import { http } from "@/utils/http";
import { getAccessToken } from "@/utils/auth";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export type TraceHealthState = "disabled" | "degraded" | "ready";
export type TraceDirection = "inbound" | "outbound";

export interface TraceHealth {
    state: TraceHealthState;
    queueDepth: number;
    queueCapacity: number;
    dropped: number;
    lastError?: string;
    lastSuccessAt?: string;
    currentGap?: TraceGap;
    lastGap?: TraceGap;
}

export interface TraceGap {
    startedAt: string;
    endedAt?: string;
    reason: string;
    eventCount: number;
}

export interface TraceMessageSummary {
    eventId: string;
    occurredAt: string;
    direction: TraceDirection;
    transport: string;
    localAddr: string;
    remoteAddr: string;
    deviceId: string;
    method: string;
    statusCode: number;
    callId: string;
    cseq: number;
    cseqMethod: string;
    fromUri?: string;
    toUri?: string;
    userAgent?: string;
    malformed: boolean;
    parseError?: string;
}

export interface TraceMessageDetail extends TraceMessageSummary {
    payload: string;
    sensitive: boolean;
}

export interface TraceMessagePage {
    items: TraceMessageSummary[];
    nextCursor?: string;
}

export interface TraceMessageQuery {
    from: string;
    to: string;
    deviceId?: string;
    deviceIds?: string;
    callId?: string;
    keyword?: string;
    direction?: TraceDirection;
    method?: string;
    statusCode?: number;
    statusMin?: number;
    statusMax?: number;
    cursor?: string;
    limit?: number;
}

export interface TraceSessionSummary {
    day: string;
    deviceId: string;
    callId: string;
    firstAt: string;
    lastAt: string;
    messageCount: number;
    inboundCount: number;
    outboundCount: number;
    methods: string[];
    finalStatus: number;
    firstMethod: string;
    fromUri?: string;
    toUri?: string;
    sourceAddr?: string;
    destinationAddr?: string;
    requestCount: number;
    finalResponseCount: number;
    originalAvailable: boolean;
    originalExpiresAt: string;
    missingResponse: boolean;
    anomaly: boolean;
}

export interface TraceSessionStats {
    total: number;
    anomaly: number;
    registerFail: number;
    invitePending: number;
}

export interface TraceSessionQuery {
    from: string;
    to: string;
    deviceId?: string;
    deviceIds?: string;
    callId?: string;
    keyword?: string;
    anomaly?: boolean;
    limit?: number;
}

export type TraceCaptureStatus = "disabled" | "active" | "ended";

export interface TraceCapture {
    id: string;
    deviceId: number;
    deviceCode: string;
    createdBy: number;
    startedAt: string;
    plannedEndAt: string;
    endedAt?: string | null;
    endReason: string;
}

export interface TraceCaptureFilter {
    captureId: string;
    deviceId: string;
    from: string;
    to: string;
}

export interface TraceCaptureResult {
    capture?: TraceCapture | null;
    reused?: boolean;
    status: TraceCaptureStatus;
    filter?: TraceCaptureFilter;
}

export const fetchTraceHealth = () =>
    http.request<BaseResult<TraceHealth>>("get", baseUrlApi("gb28181/sip-traces/health"));

export const listTraceMessages = (params: TraceMessageQuery) =>
    http.request<BaseResult<TraceMessagePage>>("get", baseUrlApi("gb28181/sip-traces/messages"), { params });

export const getTraceMessage = (id: string, options: { sensitive?: boolean; purpose?: string } = {}) =>
    http.request<BaseResult<TraceMessageDetail>>("get", baseUrlApi(`gb28181/sip-traces/messages/${id}`), {
        params: { sensitive: options.sensitive ? "true" : undefined, purpose: options.purpose || undefined }
    });

export const listTraceSessions = (params: TraceSessionQuery) =>
    http.request<BaseResult<{ items: TraceSessionSummary[] }>>("get", baseUrlApi("gb28181/sip-traces/sessions"), { params });

export const fetchTraceSessionStats = (params: TraceSessionQuery) =>
    http.request<BaseResult<TraceSessionStats>>("get", baseUrlApi("gb28181/sip-traces/sessions/stats"), { params });

/**
 * SSE 实时报文流 URL 构造(基础路径固定,由调用侧 new EventSource 建立连接)
 * EventSource 不能自定义 header,JWT 只能通过 URL 参数 ?token= 传递(后端 middleware 支持双通道)
 */
export function buildTraceStreamUrl(params: {
    deviceId?: string;
    callId?: string;
    method?: string;
    sensitive?: boolean;
    purpose?: string;
} = {}): string {
    const search = new URLSearchParams();
    // 自动附加 access token(header 走不通的场景 URL 兜底)
    const tokenData = getAccessToken();
    if (tokenData?.accessToken) {
        search.set("token", tokenData.accessToken);
    }
    if (params.deviceId) search.set("deviceId", params.deviceId);
    if (params.callId) search.set("callId", params.callId);
    if (params.method) search.set("method", params.method);
    if (params.sensitive) {
        search.set("sensitive", "true");
        if (params.purpose) search.set("purpose", params.purpose);
    }
    const qs = search.toString();
    return baseUrlApi("gb28181/sip-traces/stream") + (qs ? `?${qs}` : "");
}

export const listTraceSessionMessages = (callId: string, params: TraceMessageQuery) =>
    http.request<BaseResult<TraceMessagePage>>(
        "get",
        baseUrlApi(`gb28181/sip-traces/sessions/${encodeURIComponent(callId)}/messages`),
        { params }
    );

export const getActiveTraceCapture = (deviceId: number) =>
    http.request<BaseResult<TraceCaptureResult>>(
        "get",
        baseUrlApi(`gb28181/device-mgmt/device/${deviceId}/sip-trace-capture`)
    );

export const startTraceCapture = (deviceId: number) =>
    http.request<BaseResult<TraceCaptureResult>>(
        "post",
        baseUrlApi(`gb28181/device-mgmt/device/${deviceId}/sip-trace-captures`)
    );

export const stopTraceCapture = (captureId: string) =>
    http.request<BaseResult<TraceCaptureResult>>(
        "post",
        baseUrlApi(`gb28181/sip-traces/captures/${captureId}/stop`)
    );
