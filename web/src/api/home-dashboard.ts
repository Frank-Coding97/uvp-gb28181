import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";
import type { DashboardLayout } from "@/views/home/dashboardRegistry";

export interface StoredDashboardLayout {
  revision: number;
  layout: DashboardLayout;
}

export type HomeDashboardGroup = "realtime" | "assets" | "aggregate";

export interface HomeDashboardSection<T> {
  status: "ok" | "empty" | "partial" | "unavailable" | "forbidden" | "disabled";
  asOf: string;
  coverage: "complete" | "partial" | "not_started";
  data: T;
}

export interface TodayTrafficSummary {
  upstreamBytes: number;
  downstreamBytes: number;
  status: HomeDashboardSection<unknown>["status"];
  coverage: HomeDashboardSection<unknown>["coverage"];
}

export interface OnlineRateSummary {
  total: number;
  online: number;
  offline: number;
  rate: number;
}

export interface AssetOnlineSummary {
  devices: OnlineRateSummary;
  channels: OnlineRateSummary;
  asOf: string;
}

export interface PlaySuccessSummary {
  attempts: number;
  success: number;
  failure: number;
  started: number;
  staleStarted: number;
  rate: number | null;
  status: HomeDashboardSection<unknown>["status"];
  coverage: HomeDashboardSection<unknown>["coverage"];
  asOf: string;
}

export interface SIPMinuteSummary {
  bucketStart: string;
  requests: number;
  success: number;
  failure: number;
}

export interface SIPMetricSummary {
  rpm: number;
  todayRequests: number;
  transactions: number;
  success: number;
  failure: number;
  series: SIPMinuteSummary[];
  status: HomeDashboardSection<unknown>["status"];
  coverage: HomeDashboardSection<unknown>["coverage"];
  asOf: string;
}

export interface HomeDashboardSummary {
  assets?: HomeDashboardSection<AssetOnlineSummary>;
  sip?: HomeDashboardSection<SIPMetricSummary>;
  play?: HomeDashboardSection<PlaySuccessSummary>;
  traffic?: HomeDashboardSection<TodayTrafficSummary>;
  bindings?: HomeDashboardSection<HomeRuntimeBinding[]>;
}

export interface HomeRuntimeBinding {
  streamId: string;
  deviceId: string;
  deviceName: string;
  channelId: string;
  channelName: string;
}

export const getHomeDashboardLayout = () =>
  http.request<BaseResult<StoredDashboardLayout>>("get", baseUrlApi("gb28181/home/layout"));

export const saveHomeDashboardLayout = (revision: number, layout: DashboardLayout) =>
  http.request<BaseResult<StoredDashboardLayout>>("put", baseUrlApi("gb28181/home/layout"), {
    data: { revision, layout }
  });

export const resetHomeDashboardLayout = () =>
  http.request<BaseResult<StoredDashboardLayout>>("delete", baseUrlApi("gb28181/home/layout"));

export const getHomeDashboardSummary = (groups: HomeDashboardGroup[] = ["aggregate"]) =>
  http.request<BaseResult<HomeDashboardSummary>>("get", baseUrlApi("gb28181/home/summary"), {
    params: { groups: groups.join(",") }
  });
