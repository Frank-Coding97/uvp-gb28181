import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { getDashboardDrilldown } from "./home-dashboard-drilldown";

describe("home dashboard drilldown API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("maps SIP cards to one history endpoint and forwards AbortSignal", async () => {
    const controller = new AbortController();
    await getDashboardDrilldown("sip-rpm", "7d", controller.signal);
    await getDashboardDrilldown("sip-today", "1h", controller.signal);
    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/home/drilldown/sip", { params: { range: "7d" }, signal: controller.signal });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/home/drilldown/sip", { params: { range: "1h" }, signal: controller.signal });
  });

  it("maps play and paged traffic without offering traffic 1h", async () => {
    await getDashboardDrilldown("play-success-24h", "24h");
    await getDashboardDrilldown("media-traffic-today", "7d", undefined, 2, 50);
    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/home/drilldown/play", { params: { range: "24h" }, signal: undefined });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/home/drilldown/traffic", {
      params: { range: "7d", page: 2, pageSize: 50 }, signal: undefined
    });
    await expect(getDashboardDrilldown("media-traffic-today", "1h")).rejects.toThrow("仅支持");
    expect(request).toHaveBeenCalledTimes(2);
  });
});
