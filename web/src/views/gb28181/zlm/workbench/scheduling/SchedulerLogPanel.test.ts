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
  "a-select": {
    props: ["modelValue", "options", "disabled"],
    emits: ["change"],
    template: "<select :value='modelValue' :disabled='disabled' @change=\"$emit('change', $event.target.value)\"><option v-for='option in options' :key='option.value' :value='option.value'>{{ option.label }}</option></select>"
  },
  "a-spin": { template: "<span />" },
  "a-switch": { template: "<button><slot /></button>" },
  "a-table": { props: ["data"], template: "<div data-testid='log-table'><span v-for='entry in data' :key='entry.id' data-testid='log-row'>{{ entry.streamID }}</span><slot name='columns' /><slot name='empty' /></div>" },
  "a-table-column": { template: "<div><slot name='cell' :record='{}' /></div>" },
  "a-pagination": { template: "<button data-testid='log-pagination' @click=\"$emit('change', 2)\">下一页</button>" },
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
    expect(wrapper.find("select").exists()).toBe(true);
    wrapper.unmount();
  });

  it("moves node scope into the filter row and emits node changes", async () => {
    api.listSchedulerLogs.mockResolvedValue({ code: 0, data: { list: [], limit: 100 } });
    const wrapper = mount(SchedulerLogPanel, {
      props: {
        active: true,
        scope: 2,
        nodes: [{ id: 2, name: "zlm-220", state: "active" }, { id: 3, name: "zlm-221", state: "active" }]
      },
      global: { stubs }
    });
    await flushPromises();
    const nodeSelect = wrapper.get("[data-testid='node-filter']");
    expect(nodeSelect.attributes("disabled")).toBeUndefined();
    expect(wrapper.text()).toContain("zlm-220 · 在线");
    await nodeSelect.setValue("3");
    await flushPromises();
    expect(wrapper.emitted("update:scope")?.at(-1)).toEqual([3]);
    wrapper.unmount();
  });

  it("paginates the current server-filtered sample without changing the query", async () => {
    const entries = Array.from({ length: 25 }, (_, index) => ({
      id: index + 1,
      happenedAt: `2026-09-18T11:${String(index).padStart(2, "0")}:00Z`,
      algorithm: "roundrobin",
      nodeID: 2,
      nodeName: "zlm-220",
      streamID: `stream-${index + 1}`,
      deviceID: "device-1",
      channelID: "channel-1",
      errorMessage: ""
    }));
    api.listSchedulerLogs.mockResolvedValue({ code: 0, data: { list: entries, limit: 100 } });
    const wrapper = mount(SchedulerLogPanel, {
      props: { active: true, nodes: [], scope: 2 },
      global: { stubs }
    });
    await flushPromises();
    expect(wrapper.findAll("[data-testid='log-row']")).toHaveLength(20);
    expect(api.listSchedulerLogs).toHaveBeenCalledTimes(1);
    await wrapper.get("[data-testid='log-pagination']").trigger("click");
    expect(wrapper.findAll("[data-testid='log-row']")).toHaveLength(5);
    expect(wrapper.text()).toContain("stream-21");
    expect(api.listSchedulerLogs).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("uses server-side filter results as the chart sample and keeps errors safe", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerLogPanel.vue"), "utf8");
    expect(source).toMatch(/listSchedulerLogs\(filter/);
    expect(source).toContain("buildSchedulerChartState");
    expect(source).toContain("MediaVChart");
    expect(source).toContain("当前筛选");
    expect(source).not.toContain("chartState.sampleCount");
    expect(source).toContain("errorPresentation.label");
    expect(source).toContain('class="time-filter"');
    expect(source).toContain("@media (max-width: 1200px)");
    expect(source).toContain("visibleLogs");
    expect(source).toContain("a-pagination");
    expect(source).toContain("s-layout-search");
    expect(source).toContain("a-range-picker");
    expect(source).toContain("a-input-search");
    expect(source).toContain("a-table");
    expect(source).toContain("uvp-data-table");
    expect(source).not.toContain('<table class="log-table">');
    expect(source).not.toContain("DECISION AUDIT");
    expect(source).not.toContain('class="panel-heading"');
    expect(source).not.toContain("sample-boundary");
    expect(source).not.toContain("error.message");
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerLog.vue"), "utf8")).toContain("SchedulerLogPanel");
  });

  it("removes the extra outer card while keeping inner data sections framed", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerLogPanel.vue"), "utf8");
    expect(source).toMatch(/\.scheduler-log-panel\s*\{[^}]*padding:\s*0;[^}]*background:\s*transparent;[^}]*border:\s*0;[^}]*border-radius:\s*0;[^}]*box-shadow:\s*none;/s);
    expect(source).toMatch(/\.scheduler-log-search\s*\{[^}]*margin-bottom:\s*14px;/s);
    expect(source).toMatch(/\.scheduler-log-search :deep\(\.arco-select-view-single\)[^}]*background:\s*var\(--uvp-search-control-bg\) !important/s);
    expect(source).toMatch(/\.scheduler-log-search :deep\(\.arco-select-view-focus\)[^}]*box-shadow:\s*var\(--uvp-search-control-focus-shadow\) !important/s);
    expect(source).toMatch(/\.log-table-panel\s*\{[^}]*background:\s*var\(--uvp-panel-bg\);[^}]*border:\s*1px solid var\(--uvp-panel-border\);[^}]*border-radius:\s*var\(--uvp-panel-radius\);[^}]*box-shadow:\s*var\(--uvp-panel-shadow\);/s);
    expect(source).toMatch(/\.log-pagination\s*\{[^}]*border-top:\s*1px solid var\(--zlm-border\);/s);
    expect(source).not.toContain(".log-filters");
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
