import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";
import type { DashboardDrilldownMetric, DashboardHistoryRange } from "@/views/home/dashboardDrilldownState";

export type DashboardHistoryStatus = "ok" | "empty" | "partial" | "unavailable" | "forbidden" | "disabled";
export type DashboardHistoryCoverage = "complete" | "partial" | "not_started";

export interface DashboardHistoryEnvelope<T> {
  status: DashboardHistoryStatus;
  asOf: string;
  scope: { type: "user-visible" | "platform" | "node"; nodeId?: number };
  coverage: DashboardHistoryCoverage;
  data: T;
}

export interface DashboardHistoryBase {
  range: DashboardHistoryRange;
  from: string;
  to: string;
  bucketSeconds: number;
  timezone: string;
  status: DashboardHistoryStatus;
  coverage: DashboardHistoryCoverage;
}

export interface SIPHistoryPoint {
  bucketStart: string;
  requests: number | null;
  transactions: number | null;
  success: number | null;
  failure: number | null;
  rpm: number | null;
}

export interface SIPHistory extends DashboardHistoryBase {
  points: SIPHistoryPoint[];
  ledger: Array<{ method: string; direction: string; requests: number; transactions: number; success: number; failure: number }>;
  gaps: Array<{ startedAt: string; endedAt: string; reason: string; droppedCount: number }>;
  todayRequests: number;
  rollingRequests: number;
}

export interface PlayHistoryPoint {
  bucketStart: string;
  success: number;
  failure: number;
  started: number;
  rate: number | null;
}

export interface PlayHistory extends DashboardHistoryBase {
  points: PlayHistoryPoint[];
  summary: { attempts: number; success: number; failure: number; started: number; staleStarted: number; rate: number | null };
  failureStages: Array<{ key: string; count: number; rate: number }>;
  reuse: Array<{ key: string; count: number; rate: number }>;
}

export interface TrafficHistoryPoint {
  bucketStart: string;
  upstreamBytes: number;
  downstreamBytes: number;
}

export interface TrafficHistory extends DashboardHistoryBase {
  points: TrafficHistoryPoint[];
  summary: { upstreamBytes: number; downstreamBytes: number };
  ledger: {
    rows: Array<{ deviceCode: string; channelCode: string; upstreamBytes: number; downstreamBytes: number }>;
    total: number;
    page: number;
    pageSize: number;
  };
  gaps: Array<{ startedAt: string; endedAt: string | null; reason: string }>;
}

export type DashboardHistoryData = SIPHistory | PlayHistory | TrafficHistory;
export type DashboardHistoryResponse = BaseResult<DashboardHistoryEnvelope<DashboardHistoryData>>;

export function getDashboardDrilldown(
  metric: DashboardDrilldownMetric,
  range: DashboardHistoryRange,
  signal?: AbortSignal,
  page = 1,
  pageSize = 20
) {
  const endpoint = metric === "sip-rpm" || metric === "sip-today" ? "sip" : metric === "play-success-24h" ? "play" : "traffic";
  if (endpoint === "traffic" && range === "1h") return Promise.reject(new Error("媒体流量仅支持 24h、7d"));
  const params: Record<string, string | number> = { range };
  if (endpoint === "traffic") Object.assign(params, { page, pageSize });
  return http.request<DashboardHistoryResponse>("get", baseUrlApi(`gb28181/home/drilldown/${endpoint}`), { params, signal });
}
