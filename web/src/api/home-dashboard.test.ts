import { beforeEach, describe, expect, it, vi } from "vitest";

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { getHomeDashboardLayout, resetHomeDashboardLayout, saveHomeDashboardLayout } from "./home-dashboard";
import { DEFAULT_DASHBOARD_LAYOUT } from "@/views/home/dashboardRegistry";

describe("home dashboard API", () => {
  beforeEach(() => request.mockReset());

  it("uses the authenticated user layout endpoints", () => {
    getHomeDashboardLayout();
    saveHomeDashboardLayout(3, DEFAULT_DASHBOARD_LAYOUT);
    resetHomeDashboardLayout();
    expect(request.mock.calls).toEqual([
      ["get", "/api/gb28181/home/layout"],
      ["put", "/api/gb28181/home/layout", { data: { revision: 3, layout: DEFAULT_DASHBOARD_LAYOUT } }],
      ["delete", "/api/gb28181/home/layout"]
    ]);
  });
});
