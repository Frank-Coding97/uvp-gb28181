import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SipDashboardCard from "./index.vue";

vi.mock("../dashboard/DashboardChart.vue", () => ({ default: { template: "<div />" } }));

const apiMocks = vi.hoisted(() => ({
  fetchSnapshot: vi.fn()
}));

vi.mock("@/api/gb28181", () => ({
  HEALTH_EMPTY: -1,
  fetchSipDashboardSnapshot: apiMocks.fetchSnapshot,
  sipDashboardStreamUrl: () => "/api/gb28181/sip/dashboard/stream"
}));

class FakeEventSource {
  onerror: (() => void) | null = null;

  addEventListener = vi.fn();

  close = vi.fn();
}

describe("SIP dashboard card", () => {
  beforeEach(() => {
    vi.stubGlobal("EventSource", FakeEventSource);
    apiMocks.fetchSnapshot.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows a visible permission state instead of rendering failed data as zero", async () => {
    apiMocks.fetchSnapshot.mockRejectedValue({ response: { status: 403 } });

    const wrapper = mount(SipDashboardCard);
    await flushPromises();

    expect(wrapper.find("[role='alert']").text()).toContain("无权查看 SIP 协议监控");
    expect(wrapper.text()).not.toContain("今日事务");
    expect(wrapper.text()).not.toContain("异常事务");
  });

  it("surfaces durable snapshot coverage without treating the SSE connection as data completeness", async () => {
    apiMocks.fetchSnapshot.mockResolvedValue({
      data: {
        health: 100,
        todayTotal: 4902,
        todayAbnormal: 0,
        pending: 0,
        transactions: [],
        pulse: { windowMinutes: 60, samples: [], abnormalWindows: [] },
        partial: true,
        asOf: 0
      }
    });

    const wrapper = mount(SipDashboardCard);
    await flushPromises();

    expect(wrapper.text()).toContain("4,902");
    expect(wrapper.text()).toContain("统计部分覆盖");
  });
});
