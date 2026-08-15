import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

export interface TrafficScopeParams {
    deviceId: string;
    channelId?: string;
    from?: string;
    to?: string;
}

export interface TrafficSummary {
    upstreamBytes: number;
    downstreamBytes: number;
    totalBytes: number;
    upstreamDurationSeconds: number;
    downstreamDurationSeconds: number;
    upstreamSessions: number;
    downstreamSessions: number;
    from: string;
    to: string;
    timezone: string;
}

export interface TrafficTrendPoint {
    date: string;
    upstreamBytes: number;
    downstreamBytes: number;
    totalBytes: number;
}

export interface TrafficRealtime {
    deviceCode: string;
    channelCode: string;
    nodeId: number;
    stream: string;
    inProgressUpstreamBytes: number;
    upstreamBytesPerSecond: number;
    readerCount: number;
    estimatedDownstreamBytesPerSec: number;
    sampledAt: string;
}

export interface TrafficCoverage {
    coverage: "complete" | "partial" | "not_started";
    statisticsStartedAt?: string | null;
    gaps: Array<{ id: number; reason: string; startedAt: string; endedAt?: string | null }>;
}

export interface CurrentViewer {
    channelId: string;
    schema: string;
    remote: string;
    localPort: number;
    id: string;
    type: string;
    kickable: boolean;
}

export const getTrafficSummary = (params: TrafficScopeParams, signal?: AbortSignal) =>
    http.request<BaseResult<TrafficSummary>>("get", baseUrlApi("gb28181/device-traffic/summary"), { params, signal });

export const getTrafficTrend = (params: TrafficScopeParams, signal?: AbortSignal) =>
    http.request<BaseResult<{ list: TrafficTrendPoint[]; timezone: string }>>("get", baseUrlApi("gb28181/device-traffic/trend"), { params, signal });

export const getTrafficRealtime = (params: TrafficScopeParams, signal?: AbortSignal) =>
    http.request<BaseResult<{ list: TrafficRealtime[]; estimated: boolean }>>("get", baseUrlApi("gb28181/device-traffic/realtime"), { params, signal });

export const getTrafficCoverage = (params: TrafficScopeParams, signal?: AbortSignal) =>
    http.request<BaseResult<TrafficCoverage>>("get", baseUrlApi("gb28181/device-traffic/coverage"), { params, signal });

export const getCurrentViewers = (params: TrafficScopeParams, signal?: AbortSignal) =>
    http.request<BaseResult<{ list: CurrentViewer[]; total: number; canKick: boolean }>>("get", baseUrlApi("gb28181/device-traffic/viewers"), { params, signal });

export const kickCurrentViewer = (params: TrafficScopeParams, data: { id: string; schema: string }) =>
    http.request<BaseResult<{ kicked: boolean }>>("post", baseUrlApi("gb28181/device-traffic/viewers/kick"), { params, data });
