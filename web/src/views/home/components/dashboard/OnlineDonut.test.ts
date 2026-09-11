import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";

vi.mock("./DashboardChart.vue", () => ({
  default: {
    name: "DashboardChart",
    props: ["spec", "title", "summary"],
    template: '<div data-dashboard-chart />'
  }
}));

import OnlineDonut from "./OnlineDonut.vue";

function mountDonut(online: number, total: number) {
  return mount(OnlineDonut, { props: { online, total, label: "设备在线率" } });
}

function chartSpec(wrapper: ReturnType<typeof mountDonut>) {
  return wrapper.getComponent({ name: "DashboardChart" }).props("spec") as Record<string, any>;
}

describe("OnlineDonut", () => {
  it("uses a solid circular progress track with rounded caps and the existing legend data", () => {
    const wrapper = mountDonut(2, 68);
    const spec = chartSpec(wrapper);

    expect(spec.type).toBe("circularProgress");
    expect(spec.categoryField).toBe("category");
    expect(spec.valueField).toBe("value");
    expect(spec.data[0].values[0].category).toBe("在线率");
    expect(spec.data[0].values[0].value).toBeCloseTo(2 / 68, 12);
    expect(spec.color).toEqual(["var(--uvp-brand-cyan)"]);
    expect(spec.outerRadius).toBe(0.94);
    expect(spec.innerRadius).toBe(0.7);
    expect(spec.startAngle).toBe(-90);
    expect(spec.endAngle).toBe(270);
    expect(spec.roundCap).toBe(true);
    expect(spec.cornerRadius).toBe(8);
    expect(spec.progress.style).toMatchObject({ fill: "var(--uvp-brand-cyan)", fillOpacity: 1 });
    expect(spec.track.style).toMatchObject({ fill: "var(--uvp-panel-border)", fillOpacity: 0.82 });
    expect(spec.axes).toEqual([
      { orient: "angle", type: "linear", min: 0, max: 1, visible: false },
      { orient: "radius", type: "band", visible: false }
    ]);
    expect(wrapper.get(".donut").attributes("style") ?? "").not.toContain("conic-gradient");
    expect(wrapper.get(".donut-center strong").text()).toBe("2.9%");
    expect(wrapper.get(".legend").text()).toContain("在线2");
    expect(wrapper.get(".legend").text()).toContain("离线66");
    expect(wrapper.get(".legend").text()).toContain("总计68");
  });

  it.each([
    { online: 0, total: 0, ratio: "0.0%", values: [{ category: "在线率", value: 0 }] },
    { online: 0, total: 10, ratio: "0.0%", values: [{ category: "在线率", value: 0 }] },
    { online: 10, total: 10, ratio: "100.0%", values: [{ category: "在线率", value: 1 }] }
  ])("keeps the $ratio boundary without adding a phantom segment", ({ online, total, ratio, values }) => {
    const wrapper = mountDonut(online, total);
    const spec = chartSpec(wrapper);

    expect(wrapper.get(".donut-center strong").text()).toBe(ratio);
    expect(spec.data[0].values).toEqual(values);
    expect(spec.progress.style.fillOpacity).toBe(online > 0 ? 1 : 0);
    expect(spec.track.style.fill).toBe("var(--uvp-panel-border)");
  });

  it("updates the chart data and percentage when props change", async () => {
    const wrapper = mountDonut(2, 68);

    await wrapper.setProps({ online: 9, total: 12 });
    await nextTick();

    expect(chartSpec(wrapper).data[0].values).toEqual([{ category: "在线率", value: 0.75 }]);
    expect(wrapper.get(".donut-center strong").text()).toBe("75.0%");

    await wrapper.setProps({ online: 0 });
    expect(chartSpec(wrapper).progress.style.fillOpacity).toBe(0);
    expect(wrapper.get(".donut-center strong").text()).toBe("0.0%");

    await wrapper.setProps({ online: 1 });
    expect(chartSpec(wrapper).progress.style.fillOpacity).toBe(1);
  });
});
