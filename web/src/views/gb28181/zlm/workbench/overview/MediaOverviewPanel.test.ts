import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

const overviewApi = vi.hoisted(() => ({ getZLMOverview: vi.fn() }));
const router = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("@/api/gb28181-zlm-runtime", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/gb28181-zlm-runtime")>()),
  getZLMOverview: overviewApi.getZLMOverview
}));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => router
}));
vi.mock("lucide-vue-next", () => {
  const icon = { template: "<i aria-hidden='true' />" };
  return { Activity: icon, AlertTriangle: icon, ArrowRight: icon, Radio: icon, Server: icon, Users: icon };
});
vi.mock("@visactor/vchart", () => ({ default: class {} }));

import MediaOverviewPanel from "./MediaOverviewPanel.vue";

function snapshot() {
  return {
    nodes: [{
      nodeId: 2,
      name: "边缘节点 2",
      state: "active",
      status: "fresh",
      freshness: "fresh",
      asOf: "2026-08-30T10:00:00Z",
      heartbeatFreshness: "fresh",
      metrics: {
        mediaSourceCount: 1,
        multiMediaSourceMuxerCount: 1,
        tcpServerCount: 1,
        tcpSessionCount: 2,
        udpServerCount: 0,
        udpSessionCount: 0,
        tcpClientCount: 0,
        socketCount: 3,
        networkSessionCount: 2,
        netThreadLoad: 0.2,
        workThreadLoad: 0.1
      },
      metricsComplete: true,
      mediaFreshness: "fresh",
      streams: []
    }],
    streams: [],
    metrics: {
      sampledNodeCount: 1,
      mediaSourceCount: 1,
      multiMediaSourceMuxerCount: 1,
      tcpServerCount: 1,
      tcpSessionCount: 2,
      udpServerCount: 0,
      udpSessionCount: 0,
      tcpClientCount: 0,
      socketCount: 3,
      networkSessionCount: 2,
      netThreadLoadAvg: 0.2,
      workThreadLoadAvg: 0.1,
      streamCount: 0
    },
    partial: false,
    asOf: "2026-08-30T10:00:00Z",
    metricsSampledNodeIds: [2],
    mediaSampledNodeIds: [2],
    successfulNodeIds: [2],
    failedNodeIds: []
  };
}

describe("MediaOverviewPanel", () => {
  it("loads the real overview endpoint once and renders KPI cards from the returned snapshot", async () => {
    overviewApi.getZLMOverview.mockResolvedValue({ code: 0, message: "", data: snapshot() });
    const wrapper = mount(MediaOverviewPanel, {
      global: {
        stubs: {
          MediaVChart: { template: "<div class='chart-stub' />" },
          StatCard: { props: ["title", "valueText"], template: "<div class='stat-card-stub'>{{ title }} {{ valueText }}</div>" },
          "a-table": { template: "<div><slot name='columns' /></div>" },
          "a-table-column": { template: "<div><slot /></div>" }
        }
      }
    });
    await flushPromises();

    expect(overviewApi.getZLMOverview).toHaveBeenCalledOnce();
    expect(wrapper.findAll(".stat-card-stub")).toHaveLength(6);
    expect(wrapper.text()).toContain("网络会话 2");
    expect(wrapper.text()).toContain("未知");
    wrapper.unmount();
  });

  it("retains the last successful snapshot when a later refresh fails", async () => {
    overviewApi.getZLMOverview.mockResolvedValueOnce({ code: 0, message: "", data: snapshot() });
    const wrapper = mount(MediaOverviewPanel, {
      global: {
        stubs: {
          MediaVChart: true,
          StatCard: { props: ["title", "valueText"], template: "<div class='stat-card-stub'>{{ title }} {{ valueText }}</div>" },
          "a-table": true,
          "a-table-column": true
        }
      }
    });
    await flushPromises();
    overviewApi.getZLMOverview.mockRejectedValueOnce(new Error("upstream unavailable"));
    (wrapper.vm as unknown as { refresh: () => void }).refresh();
    await flushPromises();

    expect(wrapper.text()).toContain("上一次成功数据");
    expect(wrapper.text()).toContain("网络会话 2");
    wrapper.unmount();
  });

  it("uses one overview request, shared chart adapters, and canonical drill-down locations", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/overview/MediaOverviewPanel.vue"), "utf8");
    expect(source).toContain("getZLMOverview");
    expect(source).toContain("useZLMRuntimePolling");
    expect(source).toContain("MediaVChart");
    expect(source).toContain("buildOverviewChartState");
    expect(source).toContain("nodeOverviewLocation");
    expect(source).toContain("streamOverviewLocation");
    expect(source).not.toContain("/gb28181/zlm/streams");
    expect(source).not.toContain("/gb28181/zlm/nodes/");
  });

  it("renders six KPI categories and explicit unknown/partial labels", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/overview/MediaOverviewPanel.vue"), "utf8");
    const stateSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/overview/overviewState.ts"), "utf8");
    for (const label of ["媒体节点", "在线媒体流", "网络会话", "观看者", "吞吐", "录制中"]) {
      expect(stateSource).toContain(label);
    }
    expect(stateSource).toContain("部分采样");
    expect(source).toContain("未知");
    expect(source).toContain("—");
  });
});
