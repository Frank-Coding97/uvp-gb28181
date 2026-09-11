import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";

const dashboardChartStub = vi.hoisted(() => ({
  name: "DashboardChart",
  props: ["spec", "title", "summary"],
  template: "<div class='dashboard-chart-stub'><span>{{ summary }}</span></div>"
}));
vi.mock("./DashboardChart.vue", () => ({ default: dashboardChartStub }));

import MediaRateArea from "./MediaRateArea.vue";

const source = readFileSync(resolve(process.cwd(), "src/views/home/components/dashboard/MediaRateArea.vue"), "utf8");

interface MediaChartSpecLike {
  type: string;
  data?: Array<{ values: Array<Record<string, unknown>> }>;
  series?: Array<Record<string, any>>;
  axes?: Array<Record<string, any>>;
  tooltip?: Record<string, any>;
  crosshair?: Record<string, any>;
}

const sampledAt = (time: string) => Date.parse(`2026-09-05T${time}+08:00`);
const mountChart = (samples: Array<{ upstream: number; downstream: number; sampledAt: number }>) => mount(MediaRateArea, {
  props: { samples },
  global: { stubs: { DashboardChart: dashboardChartStub } }
});
const getSpec = (wrapper: ReturnType<typeof mount>): MediaChartSpecLike => wrapper.getComponent(dashboardChartStub).props("spec") as MediaChartSpecLike;

describe("MediaRateArea", () => {
  it("plots upstream and downstream together as smooth five-minute area series", () => {
    const samples = [
      { upstream: 1, downstream: 2, sampledAt: sampledAt("10:00:00") },
      { upstream: 4, downstream: 6, sampledAt: sampledAt("10:02:00") },
      { upstream: 2, downstream: 3, sampledAt: sampledAt("10:04:00") },
      { upstream: 5, downstream: 8, sampledAt: sampledAt("10:05:00") }
    ];
    const wrapper = mountChart(samples);
    const spec = getSpec(wrapper);
    const axes = spec.axes ?? [];

    expect(spec.type).toBe("common");
    expect(spec.data?.[0].values).toEqual(samples);
    expect(spec.series).toHaveLength(2);
    expect(spec.series?.[0]).toMatchObject({
      type: "area",
      yField: "upstream",
      line: { style: { curveType: "monotone", stroke: "var(--uvp-brand)" } },
      area: {
        style: {
          curveType: "monotone",
          fillOpacity: 0.22,
          fill: {
            gradient: "linear",
            stops: [
              { offset: 0, color: "var(--uvp-brand)", opacity: 0.78 },
              { offset: 0.68, color: "var(--uvp-brand)", opacity: 0.24 },
              { offset: 1, color: "var(--uvp-brand)", opacity: 0.04 }
            ]
          }
        }
      },
      point: { visible: true }
    });
    expect(spec.series?.[1]).toMatchObject({
      type: "area",
      yField: "downstream",
      line: { style: { curveType: "monotone", stroke: "var(--uvp-brand-cyan)" } },
      area: {
        style: {
          curveType: "monotone",
          fillOpacity: 0.18,
          fill: {
            gradient: "linear",
            stops: [
              { offset: 0, color: "var(--uvp-brand-cyan)", opacity: 0.78 },
              { offset: 0.68, color: "var(--uvp-brand-cyan)", opacity: 0.24 },
              { offset: 1, color: "var(--uvp-brand-cyan)", opacity: 0.04 }
            ]
          }
        }
      },
      point: { visible: true }
    });
    expect(axes[0]).toMatchObject({ orient: "left", min: 0, label: { style: { fill: "var(--uvp-text-tertiary)" } }, grid: { style: { stroke: "var(--uvp-panel-border)" } } });
    expect(axes[1]).toMatchObject({
      orient: "bottom",
      type: "time",
      nice: false,
      min: sampledAt("10:00:00"),
      max: sampledAt("10:05:00")
    });
    expect(axes[1].layers[0]).toMatchObject({ tickCount: 3, timeFormat: "%H:%M", timeFormatMode: "local" });
    expect(spec.crosshair).toMatchObject({ xField: { visible: true } });
    expect(source).not.toContain("<svg");
    expect(source).toContain("height:calc(100% - 74px)");
    expect(source).toContain("min-height:150px");
    expect(source).toContain("overflow:hidden");
    expect(source).not.toContain('color: "transparent"');
  });

  it("formats byte-rate axes and tooltip values through the chart spec", () => {
    const wrapper = mountChart([{ upstream: 1, downstream: 2, sampledAt: sampledAt("10:05:00") }]);
    const spec = getSpec(wrapper);
    const axisFormatter = spec.axes?.[0].label.formatMethod as (value: number) => string;
    const upstreamTooltip = spec.series?.[0].tooltip.dimension;
    const downstreamTooltip = spec.series?.[1].tooltip.dimension;

    expect(axisFormatter(12)).toBe("12 B/s");
    expect(axisFormatter(0.5)).toBe("0.5 B/s");
    expect(axisFormatter(1024)).toBe("1.0 KB/s");
    expect(axisFormatter(1024 ** 2)).toBe("1.0 MB/s");
    expect(axisFormatter(1024 ** 3)).toBe("1.0 GB/s");
    expect(spec.tooltip?.activeType).toBe("dimension");
    expect(upstreamTooltip.content[0].key).toBe("实时上行");
    expect(upstreamTooltip.content[0].value({ upstream: 2048 })).toBe("2.0 KB/s");
    expect(downstreamTooltip.content[0].key).toBe("实时下行");
    expect(downstreamTooltip.content[0].value({ downstream: 4096 })).toBe("4.0 KB/s");
  });

  it("does not invent an empty series and states that no samples are available", () => {
    const wrapper = mountChart([]);

    expect(wrapper.get("[role='status']").text()).toContain("暂无采样");
    expect(wrapper.findComponent(dashboardChartStub).exists()).toBe(false);
  });

  it("keeps a single real sample visible inside the five-minute domain", () => {
    const sample = { upstream: 4096, downstream: 8192, sampledAt: sampledAt("10:05:00") };
    const wrapper = mountChart([sample]);
    const spec = getSpec(wrapper);
    const upstreamPointSize = spec.series?.[0].point.style.size as (datum: { sampledAt: number }) => number;
    const downstreamPointSize = spec.series?.[1].point.style.size as (datum: { sampledAt: number }) => number;

    expect(spec.data?.[0].values).toEqual([sample]);
    expect(upstreamPointSize(sample)).toBeGreaterThan(0);
    expect(downstreamPointSize(sample)).toBeGreaterThan(0);
    expect(spec.axes?.[1]).toMatchObject({ min: sampledAt("10:00:00"), max: sampledAt("10:05:00") });
  });

  it("updates both chart series when samples change", async () => {
    const wrapper = mountChart([{ upstream: 1024, downstream: 2048, sampledAt: sampledAt("10:01:00") }]);
    await wrapper.setProps({
      samples: [
        { upstream: 2048, downstream: 4096, sampledAt: sampledAt("10:03:00") },
        { upstream: 8192, downstream: 16384, sampledAt: sampledAt("10:05:00") }
      ]
    });
    await nextTick();

    const spec = getSpec(wrapper);
    expect(spec.data?.[0].values).toEqual([
      { upstream: 2048, downstream: 4096, sampledAt: sampledAt("10:03:00") },
      { upstream: 8192, downstream: 16384, sampledAt: sampledAt("10:05:00") }
    ]);
    expect(spec.axes?.[1]).toMatchObject({ min: sampledAt("10:00:00"), max: sampledAt("10:05:00") });
  });
});
