import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/views/gb28181/zlm/workbench/components/MediaVChart.vue", () => ({
  default: { props: ["spec", "status", "summary", "asOf"], template: `<div class="chart-stub" :data-status="status" :data-as-of="asOf || ''">{{ summary }}</div>` }
}));

import DashboardDrilldownDialog from "./DashboardDrilldownDialog.vue";

const AModal = { props: ["visible", "title", "modalClass"], emits: ["cancel"], template: `<section v-if="visible" class="modal-stub" :data-modal-class="modalClass"><h2>{{ title }}</h2><slot /></section>` };
const result = {
  status: "partial" as const, asOf: "2026-09-04T10:00:00+08:00", scope: { type: "platform" as const }, coverage: "partial" as const,
  data: {
    range: "24h" as const, from: "", to: "", bucketSeconds: 300, timezone: "Asia/Shanghai", status: "partial" as const, coverage: "partial" as const,
    points: [], ledger: [{ method: "REGISTER", direction: "in", requests: 10, transactions: 9, success: 8, failure: 1 }], gaps: [], todayRequests: 10, rollingRequests: 10
  }
};

describe("DashboardDrilldownDialog", () => {
  it("shows range controls, partial state, chart and ledger", async () => {
    const wrapper = mount(DashboardDrilldownDialog, {
      props: { visible: true, metric: "sip-rpm", range: "24h", ranges: ["1h", "24h", "7d"], loading: false, stale: false, error: "", result },
      global: { stubs: { "a-modal": AModal } }
    });
    expect(wrapper.text()).toContain("统计覆盖不完整");
    expect(wrapper.text()).toContain("REGISTER");
    expect(wrapper.get(".modal-stub").attributes("data-modal-class")).toBe("uvp-system-dialog");
    expect(wrapper.get(".chart-stub").attributes("data-status")).toBe("partial");
    const buttons = wrapper.findAll(".drilldown-ranges button");
    expect(buttons).toHaveLength(3);
    await buttons[0].trigger("click");
    expect(wrapper.emitted("range")?.[0]).toEqual(["1h"]);
  });

  it("keeps last successful result visible when refresh becomes stale", () => {
    const wrapper = mount(DashboardDrilldownDialog, {
      props: { visible: true, metric: "sip-today", range: "24h", ranges: ["1h", "24h", "7d"], loading: false, stale: true, error: "network", result },
      global: { stubs: { "a-modal": AModal } }
    });
    expect(wrapper.text()).toContain("继续展示上次数据");
    expect(wrapper.text()).toContain("REGISTER");
  });

  it("shows only the area chart for media traffic", () => {
    const wrapper = mount(DashboardDrilldownDialog, {
      props: {
        visible: true, metric: "media-traffic-today", range: "24h", ranges: ["24h", "7d"], loading: false, stale: false, error: "",
        result: {
          status: "partial", asOf: "2026-09-05T10:00:00+08:00", scope: { type: "platform" }, coverage: "partial",
          data: {
            range: "24h", from: "", to: "", bucketSeconds: 3600, timezone: "Asia/Shanghai", status: "partial", coverage: "partial",
            points: [{ bucketStart: "2026-09-05T09:00:00+08:00", upstreamBytes: 1024, downstreamBytes: 512 }],
            gaps: [],
            summary: { upstreamBytes: 1024, downstreamBytes: 512 },
            ledger: { total: 1, page: 1, pageSize: 20, rows: [{ deviceCode: "device-1", channelCode: "channel-1", upstreamBytes: 1024, downstreamBytes: 512 }] }
          }
        }
      },
      global: { stubs: { "a-modal": AModal } }
    });
    expect(wrapper.get(".chart-stub").text()).toContain("Y 轴为每个时间桶累计流量；上行 1.0 KB，已结算下行 512 B");
    expect(wrapper.get(".chart-stub").classes()).toContain("traffic-chart");
    expect(wrapper.get(".chart-stub").attributes("data-as-of")).toBe("");
    expect(wrapper.text()).not.toContain("统计覆盖不完整");
    expect(wrapper.text()).not.toContain("图中断点");
    expect(wrapper.text()).not.toContain("设备 / 通道台账");
    expect(wrapper.text()).not.toContain("device-1");
  });

  it("explains stale point-play attempts instead of calling coverage complete", () => {
    const wrapper = mount(DashboardDrilldownDialog, {
      props: {
        visible: true, metric: "play-success-24h", range: "24h", ranges: ["1h", "24h", "7d"], loading: false, stale: false, error: "",
        result: {
          status: "partial", asOf: "2026-09-05T10:00:00+08:00", scope: { type: "platform" }, coverage: "partial",
          data: {
            range: "24h", from: "", to: "", bucketSeconds: 3600, timezone: "Asia/Shanghai", status: "partial", coverage: "partial",
            points: [], summary: { attempts: 1, success: 1, failure: 0, started: 1, staleStarted: 1, rate: 1 }, failureStages: [], reuse: []
          }
        }
      },
      global: { stubs: { "a-modal": AModal } }
    });
    expect(wrapper.text()).toContain("1 条点播记录超过最大超时仍未终态");
  });
});
