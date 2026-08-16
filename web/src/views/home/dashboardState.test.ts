import { describe, expect, it } from "vitest";
import {
  buildAttentionItems,
  classifyDashboardError,
  describeSipRuntime,
  loadStateLabel,
  metricText,
  ratioPercent,
  resolveDashboardRoute,
  summarizeZlmNodes
} from "./dashboardState";

describe("dashboard state", () => {
  it("keeps a real zero distinct from unavailable states", () => {
    expect(metricText("ready", 0)).toBe("0");
    expect(metricText("loading", 0)).toBe("--");
    expect(metricText("forbidden", 0)).toBe("--");
    expect(loadStateLabel("forbidden")).toBe("无权限");
    expect(loadStateLabel("disabled")).toBe("未启用");
    expect(loadStateLabel("error")).toBe("加载失败");
  });

  it("classifies permission and optional-service failures", () => {
    expect(classifyDashboardError({ response: { status: 403 } })).toBe("forbidden");
    expect(classifyDashboardError({ response: { status: 503 } }, true)).toBe("disabled");
    expect(classifyDashboardError({ response: { status: 503 } })).toBe("error");
    expect(classifyDashboardError(new Error("network"))).toBe("error");
  });

  it("derives visual ratios only from current real totals", () => {
    expect(ratioPercent(7, 10)).toBe(70);
    expect(ratioPercent(0, 0)).toBe(0);
    expect(ratioPercent(12, 10)).toBe(100);
    expect(ratioPercent(Number.NaN, 10)).toBeNull();
  });

  it("keeps an explicit fallback for unknown SIP runtime states", () => {
    expect(describeSipRuntime({ state: "running" })).toEqual({ label: "运行中", detail: "", tone: "success" });
    expect(describeSipRuntime({ state: "future_state" })).toEqual({
      label: "状态未知",
      detail: "服务状态与当前前端版本不兼容",
      tone: "warning"
    });
  });

  it("resolves the first route that is actually registered", () => {
    const routes = [{ path: "/gb28181/device-mgmt/index" }, { path: "/gb28181/device-mgmt/anomaly" }];

    expect(resolveDashboardRoute(routes, ["/gb28181/device-mgmt", "/gb28181/device-mgmt/index"])).toBe(
      "/gb28181/device-mgmt/index"
    );
    expect(resolveDashboardRoute(routes, ["/gb28181/device-mgmt/anomaly", "/gb28181/device-mgmt/index"])).toBe(
      "/gb28181/device-mgmt/anomaly"
    );
    expect(resolveDashboardRoute(routes, ["/gb28181/missing"])).toBeNull();
  });

  it("summarizes only real ZLM node fields", () => {
    expect(
      summarizeZlmNodes([
        {
          state: "active",
          nearCapacity: true,
          stats: { mediaSourceCount: 3, sessionCount: 5 }
        },
        {
          state: "offline",
          nearCapacity: true,
          stats: { mediaSourceCount: 7, sessionCount: 9 }
        },
        {
          state: "maintenance",
          nearCapacity: false,
          stats: { mediaSourceCount: 1, sessionCount: 2 }
        }
      ])
    ).toEqual({
      total: 3,
      active: 1,
      offline: 1,
      maintenance: 1,
      nearCapacity: 1,
      mediaSources: 3,
      sessions: 5
    });
  });

  it("prioritizes actionable facts and limits the panel to five rows", () => {
    const items = buildAttentionItems({
      sip: { status: "ready", state: "failed", errorSummary: "监听端口被占用" },
      devices: { status: "ready", offline: 6 },
      channels: { status: "ready", offline: 20 },
      anomalies: { status: "ready", count: 4 },
      alarms: { status: "ready", total: 3, latestDescription: "设备故障报警" },
      zlm: {
        status: "ready",
        nodes: [
          {
            state: "offline",
            nearCapacity: true,
            stats: { mediaSourceCount: 0, sessionCount: 0 }
          }
        ]
      }
    });

    expect(items).toHaveLength(5);
    expect(items.map(item => item.title)).toEqual([
      "SIP 服务运行异常",
      "1 个媒体节点离线",
      "近 24 小时收到 3 条告警",
      "4 项目录异常待处理",
      "6 台设备离线"
    ]);
    expect(items[0].detail).toBe("监听端口被占用");
    expect(buildAttentionItems({ devices: { status: "ready", offline: 0 } })).toEqual([]);
  });

  it("uses the registered device and anomaly entries for attention links", () => {
    const items = buildAttentionItems(
      {
        devices: { status: "ready", offline: 2 },
        anomalies: { status: "ready", count: 1 }
      },
      {
        deviceManagement: "/gb28181/device-mgmt/index",
        directoryAnomaly: "/gb28181/device-mgmt/anomaly"
      }
    );

    expect(items.map(item => item.route)).toEqual(["/gb28181/device-mgmt/anomaly", "/gb28181/device-mgmt/index"]);
  });
});
