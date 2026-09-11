import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  listSchedulerLogs: vi.fn(),
  listZLMNodes: vi.fn()
}));

vi.mock("@/api/gb28181-zlm", () => ({
  listSchedulerLogs: api.listSchedulerLogs,
  listZLMNodes: api.listZLMNodes
}));

vi.mock("../components/MediaVChart.vue", () => ({
  default: { template: "<div data-chart='stub' />" }
}));

import SchedulerLogPanel from "./SchedulerLogPanel.vue";
import { safeSchedulerError } from "./schedulerLogPresentation";

const stubs = {
  "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
  "a-input-search": { template: "<input />" },
  "a-range-picker": { template: "<input />" },
  "a-select": { template: "<select><slot /></select>" },
  "a-spin": { template: "<span />" },
  "a-switch": { template: "<button><slot /></button>" },
  "a-table": { template: "<div><slot name='columns' /><slot name='empty' /></div>" },
  "a-table-column": { template: "<div><slot name='cell' :record='{}' /></div>" },
  "a-tag": { template: "<span><slot /></span>" },
  "s-layout-search": { template: "<div><slot name='fields' /><slot name='actions' /></div>" },
  MediaVChart: { template: "<div data-chart='stub' />" }
};

describe("SchedulerLogPanel", () => {
  it("does not request logs or node options while inactive", async () => {
    const wrapper = mount(SchedulerLogPanel, {
      props: { active: false, nodes: [] },
      global: { stubs }
    });
    await flushPromises();
    expect(api.listSchedulerLogs).not.toHaveBeenCalled();
    expect(api.listZLMNodes).not.toHaveBeenCalled();
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(api.listSchedulerLogs).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("keeps a concrete top scope in every scheduler log request", async () => {
    api.listSchedulerLogs.mockResolvedValue({ code: 0, data: { list: [], total: 0 } });
    const wrapper = mount(SchedulerLogPanel, {
      props: { active: true, nodes: [], scope: 2 },
      global: { stubs }
    });
    await flushPromises();
    expect(api.listSchedulerLogs).toHaveBeenCalledWith(expect.objectContaining({ nodeId: 2 }));
    expect(wrapper.text()).toContain("跟随顶部节点");
    wrapper.unmount();
  });

  it("uses server-side filter results as the chart sample and keeps errors safe", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerLogPanel.vue"), "utf8");
    expect(source).toMatch(/listSchedulerLogs\(filter/);
    expect(source).toContain("buildSchedulerChartState");
    expect(source).toContain("MediaVChart");
    expect(source).toContain("当前筛选");
    expect(source).toContain("sampleCount");
    expect(source).toContain("errorPresentation.label");
    expect(source).toContain("time-range-controls");
    expect(source).toContain("@media (max-width: 1200px)");
    expect(source).not.toContain("error.message");
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerLog.vue"), "utf8")).toContain("SchedulerLogPanel");
  });

  it("redacts credentials and internal URLs before rendering a backend error", () => {
    const safe = safeSchedulerError("apiSecret=hidden token=abc https://10.0.0.8:8080/private stream failed");
    expect(safe).not.toContain("hidden");
    expect(safe).not.toContain("abc");
    expect(safe).not.toContain("10.0.0.8");
    expect(safe).toContain("敏感参数=[已隐藏]");
    expect(safe).toContain("[内部地址已隐藏]");
  });
});
