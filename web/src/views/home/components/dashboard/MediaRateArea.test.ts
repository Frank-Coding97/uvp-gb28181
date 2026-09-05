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
  color?: string[];
  data?: Array<{ values: Array<Record<string, unknown>> }>;
  seriesField?: string;
  line?: Record<string, any>;
  area?: Record<string, any>;
  point?: Record<string, any>;
  invalidType?: string;
  axes?: Array<Record<string, any>>;
  tooltip?: Record<string, any>;
  crosshair?: Record<string, any>;
}

const sampledAt = (time: string) => Date.parse(`2026-09-05T${time}+08:00`);
const mountChart = (samples: Array<{ value: number; sampledAt: number }>, color?: string) => mount(MediaRateArea, {
  props: { samples, ...(color ? { color } : {}) },
  global: { stubs: { DashboardChart: dashboardChartStub } }
});
const getSpec = (wrapper: ReturnType<typeof mount>): MediaChartSpecLike => wrapper.getComponent(dashboardChartStub).props("spec") as MediaChartSpecLike;

describe("MediaRateArea", () => {
  it("passes a continuous five-minute monotone area spec with the real sample timestamps", () => {
    const samples = [
      { value: 1, sampledAt: sampledAt("10:00:00") },
      { value: 4, sampledAt: sampledAt("10:02:00") },
      { value: 2, sampledAt: sampledAt("10:04:00") },
      { value: 5, sampledAt: sampledAt("10:05:00") }
    ];
    const wrapper = mountChart(samples, "#2563eb");
    const spec = getSpec(wrapper);
    const axes = spec.axes ?? [];

    expect(spec.type).toBe("area");
    expect(spec.color).toEqual(["#2563eb"]);
    expect(spec.data?.[0].values).toEqual(samples.map(sample => ({ ...sample, metric: "实时速率" })));
    expect(spec).toMatchObject({
      seriesField: "metric",
      invalidType: "break",
      line: { style: { curveType: "monotone" } },
      area: {
        style: {
          curveType: "monotone",
          fillOpacity: 0.28,
          fill: { gradient: "linear", stops: [{ offset: 0, color: "#2563eb" }, { offset: 1, color: "transparent" }] }
        }
      },
      point: { visible: true, state: { dimension_hover: { size: 6.4 } } }
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
  });

  it("formats byte-rate axes and tooltip values through the chart spec", () => {
    const wrapper = mountChart([{ value: 1, sampledAt: sampledAt("10:05:00") }]);
    const spec = getSpec(wrapper);
    const axisFormatter = spec.axes?.[0].label.formatMethod as (value: number) => string;
    const tooltip = spec.tooltip?.dimension;
    const tooltipValue = tooltip.content[0].value as (datum: { value: number }) => string;

    expect(axisFormatter(12)).toBe("12 B/s");
    expect(axisFormatter(0.5)).toBe("0.5 B/s");
    expect(axisFormatter(1024)).toBe("1.0 KB/s");
    expect(axisFormatter(1024 ** 2)).toBe("1.0 MB/s");
    expect(axisFormatter(1024 ** 3)).toBe("1.0 GB/s");
    expect(spec.tooltip?.activeType).toBe("dimension");
    expect(tooltip.title.valueTimeFormat).toBe("%H:%M:%S");
    expect(tooltip.title.valueTimeFormatMode).toBe("local");
    expect(tooltipValue({ value: 2048 })).toBe("2.0 KB/s");
  });

  it("does not invent an empty series and states that no samples are available", () => {
    const wrapper = mountChart([]);

    expect(wrapper.get("[role='status']").text()).toContain("暂无采样");
    expect(wrapper.findComponent(dashboardChartStub).exists()).toBe(false);
  });

  it("keeps a single real sample visible inside the five-minute domain", () => {
    const sample = { value: 4096, sampledAt: sampledAt("10:05:00") };
    const wrapper = mountChart([sample]);
    const spec = getSpec(wrapper);
    const pointSize = spec.point?.style?.size as (datum: { sampledAt: number }) => number;

    expect(spec.data?.[0].values).toEqual([{ ...sample, metric: "实时速率" }]);
    expect(spec.point?.visible).toBe(true);
    expect(pointSize(sample)).toBeGreaterThan(0);
    expect(spec.axes?.[1]).toMatchObject({ min: sampledAt("10:00:00"), max: sampledAt("10:05:00") });
  });

  it("updates the chart spec when samples and color change", async () => {
    const wrapper = mountChart([{ value: 1024, sampledAt: sampledAt("10:01:00") }], "#2563eb");
    await wrapper.setProps({
      samples: [
        { value: 2048, sampledAt: sampledAt("10:03:00") },
        { value: 8192, sampledAt: sampledAt("10:05:00") }
      ],
      color: "#0faaa6"
    });
    await nextTick();

    const spec = getSpec(wrapper);
    expect(spec.color).toEqual(["#0faaa6"]);
    expect(spec.data?.[0].values).toEqual([
      { value: 2048, sampledAt: sampledAt("10:03:00"), metric: "实时速率" },
      { value: 8192, sampledAt: sampledAt("10:05:00"), metric: "实时速率" }
    ]);
    expect(spec.axes?.[1]).toMatchObject({ min: sampledAt("10:00:00"), max: sampledAt("10:05:00") });
  });
});
