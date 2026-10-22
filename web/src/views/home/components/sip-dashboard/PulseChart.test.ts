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
  it("uses a brand-gradient area and failure line without painting abnormal windows over the chart", () => {
    const wrapper = mount(PulseChart, { props: { samples, abnormalWindows: [{ startT: 30, endT: 90 }] } });
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.type).toBe("common");
    expect(spec.data[0].values).toEqual([
      { time: 60000, messages: 2, failure: 0, known: true },
      { time: 120000, messages: 4, failure: 1, known: true }
    ]);
    expect(spec.series[0].line.style.curveType).toBe("monotone");
    expect(spec.series[0].area.style.curveType).toBe("monotone");
    // 面积观感与「媒体实时速率」一致：品牌色纵向渐隐 + fillOpacity 控制浓度。
    expect(spec.series[0].area.style.fillOpacity).toBe(0.22);
    expect(spec.series[0].area.style.fill.stops).toEqual([
      { offset: 0, color: "var(--uvp-brand)", opacity: 0.78 },
      { offset: 0.68, color: "var(--uvp-brand)", opacity: 0.24 },
      { offset: 1, color: "var(--uvp-brand)", opacity: 0.04 }
    ]);
    expect(spec.series[1].line.style).toMatchObject({ curveType: "monotone", lineDash: [2, 2] });
    expect(spec.series[1].tooltip.dimension.content[0].value({ failure: 1, known: true })).toBe("1.0%");
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

  // ⛔ 这条是「首页信令脉搏曲线断开成好几截」的护栏（2026-10-22 老板提出）：
  // 统计覆盖未知的分钟原先画成 null + invalidType: break，曲线断成碎片，
  // 仪表盘上会被读成"采集/平台掉线"。缺口必须按 0 补，曲线永远连续；
  // 「存在缺口」的信息只保留在底部提示与 tooltip 上。
  it("pads coverage gaps with zero so the curve stays continuous", () => {
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
      { time: 120000, messages: 0, failure: 0, known: false },
      { time: 180000, messages: 2, failure: 0, known: true }
    ]);
    // 连续性的硬判据：任何一个点都不允许是 null（null 才会让折线断掉）。
    for (const datum of spec.data[0].values) {
      expect(typeof datum.messages).toBe("number");
      expect(typeof datum.failure).toBe("number");
    }
    expect(spec.series[0].tooltip.dimension.content[0].value({ messages: 2, known: true })).toBe("2 条/分钟");
    expect(spec.series[0].tooltip.dimension.content[0].value({ messages: 0, known: false })).toBe("0 条/分钟（统计缺口）");
    expect(wrapper.text()).toContain("统计存在缺口（缺口按 0 计入）");
  });

  it("still draws a flat zero curve when every bucket in the window is uncovered", () => {
    const wrapper = mount(PulseChart, {
      props: { samples: [{ t: 60, msgPerSec: 0, failPct: 0, known: false }], abnormalWindows: [] }
    });

    expect(wrapper.findComponent({ name: "DashboardChart" }).exists()).toBe(true);
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.data[0].values).toEqual([{ time: 60000, messages: 0, failure: 0, known: false }]);
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
    // 标题统计仍然区分「未知」与「0」：最新完整分钟是缺口时显示 `--` 而不是沿用上一分钟；
    // 但曲线本身按 0 连续绘制（见 pads coverage gaps 那条）。
    expect(wrapper.text()).toContain("最近完整分钟 --");
    const spec = wrapper.getComponent({ name: "DashboardChart" }).props("spec");
    expect(spec.data[0].values.at(-1)).toEqual({ time: 180000, messages: 0, failure: 0, known: false });
    wrapper.unmount();
  });
});
