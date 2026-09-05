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
  it("uses a VChart pie spec with the existing donut proportions and legend data", () => {
    const wrapper = mountDonut(2, 68);
    const spec = chartSpec(wrapper);

    expect(spec.type).toBe("pie");
    expect(spec.categoryField).toBe("category");
    expect(spec.valueField).toBe("value");
    expect(spec.data[0].values).toEqual([
      { category: "在线", value: 2 },
      { category: "离线", value: 66 }
    ]);
    expect(spec.color).toEqual(["var(--uvp-brand-cyan)", "var(--uvp-list-toolbar-bg)"]);
    expect(spec.outerRadius).toBe(1);
    expect(spec.innerRadius).toBeCloseTo(78 / 112, 10);
    expect(spec.startAngle).toBe(-90);
    expect(spec.endAngle).toBe(270);
    expect(spec.pie.style.cornerRadius).toBe(0);
    expect(wrapper.get(".donut").attributes("style") ?? "").not.toContain("conic-gradient");
    expect(wrapper.get(".donut-center strong").text()).toBe("2.9%");
    expect(wrapper.get(".legend").text()).toContain("在线2");
    expect(wrapper.get(".legend").text()).toContain("离线66");
    expect(wrapper.get(".legend").text()).toContain("总计68");
  });

  it.each([
    { online: 0, total: 0, ratio: "0.0%", values: [{ category: "在线", value: 0 }, { category: "离线", value: 0 }] },
    { online: 0, total: 10, ratio: "0.0%", values: [{ category: "在线", value: 0 }, { category: "离线", value: 10 }] },
    { online: 10, total: 10, ratio: "100.0%", values: [{ category: "在线", value: 10 }, { category: "离线", value: 0 }] }
  ])("keeps the $ratio boundary without adding a phantom segment", ({ online, total, ratio, values }) => {
    const wrapper = mountDonut(online, total);
    const spec = chartSpec(wrapper);

    expect(wrapper.get(".donut-center strong").text()).toBe(ratio);
    expect(spec.data[0].values).toEqual(values);
    expect(spec.showAllZero).toBe(false);
    expect(spec.emptyPlaceholder).toMatchObject({ showEmptyCircle: true });
  });

  it("updates the chart data and percentage when props change", async () => {
    const wrapper = mountDonut(2, 68);

    await wrapper.setProps({ online: 9, total: 12 });
    await nextTick();

    expect(chartSpec(wrapper).data[0].values).toEqual([
      { category: "在线", value: 9 },
      { category: "离线", value: 3 }
    ]);
    expect(wrapper.get(".donut-center strong").text()).toBe("75.0%");
  });
});
