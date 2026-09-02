import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";
import type { DashboardLayout } from "@/views/home/dashboardRegistry";

export interface StoredDashboardLayout {
  revision: number;
  layout: DashboardLayout;
}

export const getHomeDashboardLayout = () =>
  http.request<BaseResult<StoredDashboardLayout>>("get", baseUrlApi("gb28181/home/layout"));

export const saveHomeDashboardLayout = (revision: number, layout: DashboardLayout) =>
  http.request<BaseResult<StoredDashboardLayout>>("put", baseUrlApi("gb28181/home/layout"), {
    data: { revision, layout }
  });

export const resetHomeDashboardLayout = () =>
  http.request<BaseResult<StoredDashboardLayout>>("delete", baseUrlApi("gb28181/home/layout"));
