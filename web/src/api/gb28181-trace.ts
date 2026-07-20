import { http } from "@/utils/http";
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
    requestCount: number;
    finalResponseCount: number;
    originalAvailable: boolean;
    originalExpiresAt: string;
    missingResponse: boolean;
    anomaly: boolean;
}

export interface TraceSessionQuery {
    from: string;
    to: string;
    deviceId?: string;
    deviceIds?: string;
    callId?: string;
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
