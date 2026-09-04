import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/views/gb28181/zlm/workbench/components/MediaVChart.vue", () => ({
  default: { props: ["spec", "status", "summary"], template: `<div class="chart-stub" :data-status="status">{{ summary }}</div>` }
}));

import DashboardDrilldownDialog from "./DashboardDrilldownDialog.vue";

const AModal = { props: ["visible", "title"], emits: ["cancel"], template: `<section v-if="visible" class="modal-stub"><h2>{{ title }}</h2><slot /></section>` };
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
      global: { stubs: { AModal } }
    });
    expect(wrapper.text()).toContain("统计覆盖不完整");
    expect(wrapper.text()).toContain("REGISTER");
    expect(wrapper.get(".chart-stub").attributes("data-status")).toBe("partial");
    const buttons = wrapper.findAll(".drilldown-ranges button");
    expect(buttons).toHaveLength(3);
    await buttons[0].trigger("click");
    expect(wrapper.emitted("range")?.[0]).toEqual(["1h"]);
  });

  it("keeps last successful result visible when refresh becomes stale", () => {
    const wrapper = mount(DashboardDrilldownDialog, {
      props: { visible: true, metric: "sip-today", range: "24h", ranges: ["1h", "24h", "7d"], loading: false, stale: true, error: "network", result },
      global: { stubs: { AModal } }
    });
    expect(wrapper.text()).toContain("继续展示上次数据");
    expect(wrapper.text()).toContain("REGISTER");
  });
});
