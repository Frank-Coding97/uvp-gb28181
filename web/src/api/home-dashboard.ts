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
  /**
   * 今日每小时的上行 / 下行**增量**（后端按小时桶补齐，缺数据的小时为 0）。
   * ⛔ 不要拿它当累计值画：累计值只会单调上升，形状永远是"平的"，看不出波动。
   */
  series?: TrafficSeriesPoint[];
  status: HomeDashboardSection<unknown>["status"];
  coverage: HomeDashboardSection<unknown>["coverage"];
}

export interface TrafficSeriesPoint {
  bucketStart: string;
  upstreamBytes: number;
  downstreamBytes: number;
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
  /**
   * 24 小时按小时分桶的样本（后端给，固定 24 个点，含空桶）。
   * ⛔ 空桶 `rate` 为 null：「这段时间没人点播」≠「点播全失败」，前端不能当 0% 画。
   */
  series?: PlayRateSeriesPoint[];
  status: HomeDashboardSection<unknown>["status"];
  coverage: HomeDashboardSection<unknown>["coverage"];
  asOf: string;
}

export interface PlayRateSeriesPoint {
  bucketStart: string;
  attempts: number;
  success: number;
  rate: number | null;
}

export interface SIPMinuteSummary {
  bucketStart: string;
  requests: number;
  transactions: number;
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
