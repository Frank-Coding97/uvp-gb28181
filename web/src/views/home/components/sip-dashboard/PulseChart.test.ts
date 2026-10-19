import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
vi.mock("../dashboard/DashboardChart.vue", () => ({
  default: { name: "DashboardChart", props: ["spec", "title", "summary"], template: '<div class="chart-stub" />' }
}));
import PulseChart from "./PulseChart.vue";

const samples = [
  { t: 60, msgPerSec: 2, failPct: 0 },
  { t: 120, msgPerSec: 4, failPct: 10 }
];

describe("SIP pulse chart", () => {
  it("uses a blue gradient area and failure line without painting abnormal windows over the chart", () => {
    const wrapper = mount(PulseChart, { props: { samples, abnormalWindows: [{ startT: 30, endT: 90 }] } });
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.type).toBe("common");
    expect(spec.data[0].values).toEqual([
      { time: 60000, messages: 2, failure: 0, known: true },
      { time: 120000, messages: 4, failure: 1, known: true }
    ]);
    expect(spec.series[0].line.style.curveType).toBe("monotone");
    expect(spec.series[0].area.style.curveType).toBe("monotone");
    expect(spec.series[0].area.style.fill.stops).toEqual([
      { offset: 0, color: "var(--uvp-chart-area-start)" },
      { offset: 0.68, color: "var(--uvp-chart-area-middle)" },
      { offset: 1, color: "transparent" }
    ]);
    expect(spec.series[1].line.style).toMatchObject({ curveType: "monotone", lineDash: [2, 2] });
    expect(spec.series[1].tooltip.dimension.content[0].value({ failure: 1 })).toBe("1.0%");
    expect(spec.markArea ?? []).toEqual([]);
    expect(spec.axes[0]).toMatchObject({
      visible: true,
      type: "time",
      layers: [{ tickCount: 3, timeFormat: "%H:%M", timeFormatMode: "local" }]
    });
    expect(spec.padding).toEqual({ left: 8, right: 12, top: 8, bottom: 8 });
    expect(spec.axes[1].grid).toMatchObject({ visible: true, style: { stroke: "var(--uvp-panel-border)" } });
    expect(spec.axes[1]).toMatchObject({ visible: true, seriesIndex: [0], min: 0 });
    expect(spec.axes[2]).toMatchObject({ visible: true, seriesIndex: [1], min: 0, max: 100 });
    expect(spec.axes[2].label.formatMethod(50)).toBe("50%");
    expect(wrapper.text()).toContain("峰值 4 · 最近完整分钟 4");
    expect(wrapper.text()).not.toContain("异常区间");
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

  it("distinguishes a known zero bucket from an unknown coverage gap", () => {
    const wrapper = mount(PulseChart, {
      props: {
        samples: [
          { t: 60, msgPerSec: 0, failPct: 0, known: true },
          { t: 120, msgPerSec: 0, failPct: 0, known: false },
          { t: 180, msgPerSec: 2, failPct: 0, known: true }
        ],
        abnormalWindows: []
      }
    });
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");

    expect(spec.data[0].values).toEqual([
      { time: 60000, messages: 0, failure: 0, known: true },
      { time: 120000, messages: null, failure: null, known: false },
      { time: 180000, messages: 2, failure: 0, known: true }
    ]);
    expect(wrapper.text()).toContain("统计存在缺口");
  });

  it("separates the in-progress minute using the server snapshot time and promotes it at the minute boundary", async () => {
    const wrapper = mount(PulseChart, {
      props: {
        asOf: 185,
        samples: [
          { t: 60, msgPerSec: 300, failPct: 100 },
          { t: 120, msgPerSec: 310, failPct: 100 },
          { t: 180, msgPerSec: 20, failPct: 500 }
        ],
        abnormalWindows: [{ startT: 60, endT: 180 }]
      }
    });
    let spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.data[0].values.map((d: any) => d.messages)).toEqual([300, 310]);
    expect(spec.axes[0].max).toBe(120000);
    expect(spec.markArea ?? []).toEqual([]);
    expect(wrapper.text()).toContain("最近完整分钟 310");
    expect(wrapper.text()).toContain("本分钟累计 20");
    await wrapper.setProps({ asOf: 240 });
    spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.data[0].values.map((d: any) => d.messages)).toEqual([300, 310, 20]);
    expect(wrapper.text()).toContain("最近完整分钟 20");
    expect(wrapper.text()).not.toContain("本分钟累计");
    wrapper.unmount();
  });

  it("keeps known zero minutes visible and does not turn unknown latest coverage into a previous count", async () => {
    const wrapper = mount(PulseChart, {
      props: { samples: [{ t: 60, msgPerSec: 0, failPct: 0, known: true }], abnormalWindows: [] }
    });
    expect(wrapper.findComponent({ name: "DashboardChart" }).exists()).toBe(true);
    expect(wrapper.text()).toContain("最近完整分钟 0");
    await wrapper.setProps({ samples: [samples[1], { t: 180, msgPerSec: 0, failPct: 0, known: false }] });
    expect(wrapper.text()).toContain("最近完整分钟 --");
    expect(wrapper.getComponent({ name: "DashboardChart" }).props("spec").data[0].values.at(-1).messages).toBeNull();
    wrapper.unmount();
  });
});
