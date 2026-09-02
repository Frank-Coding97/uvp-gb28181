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

export interface HomeDashboardSummary {
  traffic?: HomeDashboardSection<TodayTrafficSummary>;
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
