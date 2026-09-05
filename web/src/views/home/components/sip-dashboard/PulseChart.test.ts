import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
vi.mock("../dashboard/DashboardChart.vue", () => ({ default: { name: "DashboardChart", props: ["spec", "title", "summary"], template: '<div class="chart-stub" />' } }));
import PulseChart from "./PulseChart.vue";

const samples = [{ t: 60, msgPerSec: 2, failPct: 0 }, { t: 120, msgPerSec: 4, failPct: 10 }];

describe("SIP pulse chart", () => {
  it("uses smooth VChart series with real message/percentage values and clipped abnormal windows", () => {
    const wrapper = mount(PulseChart, { props: { samples, abnormalWindows: [{ startT: 30, endT: 90 }] } });
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.type).toBe("common");
    expect(spec.data[0].values).toEqual([{ time: 60000, messages: 2, failure: 0 }, { time: 120000, messages: 4, failure: 1 }]);
    expect(spec.series[0].line.style.curveType).toBe("monotone");
    expect(spec.series[0].area.style.curveType).toBe("monotone");
    expect(spec.series[1].line.style).toMatchObject({ curveType: "monotone", lineDash: [2, 2] });
    expect(spec.series[1].tooltip.dimension.content[0].value({ failure: 1 })).toBe("1.0%");
    expect(spec.markArea[0]).toMatchObject({ x: 60000, x1: 90000 });
    expect(wrapper.text()).toContain("峰值 4 · 当前 4");
  });

  it("keeps the no-signal state and updates when the first real sample arrives", async () => {
    const wrapper = mount(PulseChart, { props: { samples: [], abnormalWindows: [] } });
    expect(wrapper.text()).toContain("暂无信令");
    expect(wrapper.findComponent({ name: "DashboardChart" }).exists()).toBe(false);
    await wrapper.setProps({ samples: [samples[1]] });
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.data[0].values).toHaveLength(1);
    expect(spec.series[0].point.visible).toBe(true);
    expect(spec.axes[0].max).toBeGreaterThan(spec.axes[0].min);
    wrapper.unmount();
  });
});
