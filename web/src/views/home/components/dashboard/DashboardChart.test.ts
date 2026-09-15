import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("@visactor/vchart", () => ({ default: class {} }));
import DashboardChart from "./DashboardChart.vue";

const stub = { props: ["spec", "title", "summary", "showSummary"], template: '<div />' };

afterEach(() => { document.body.removeAttribute("style"); });

describe("DashboardChart", () => {
  it("resolves canvas colors from inherited CSS variables and refreshes them with the theme", async () => {
    document.body.style.setProperty("--uvp-brand", "#2563eb");
    vi.spyOn(window, "getComputedStyle").mockImplementation(() => document.body.style);
    const formatter = (value: number) => `${value} B/s`;
    const spec = { type: "area", color: ["var(--uvp-brand)"], axes: [{ label: { formatMethod: formatter } }] };
    const wrapper = mount(DashboardChart, { attachTo: document.body, props: { spec, title: "实时速率" }, global: { stubs: { MediaVChart: stub } } });
    await nextTick();
    const chart = wrapper.getComponent(stub);
    expect(chart.props("spec").color).toEqual(["#2563eb"]);
    expect(chart.props("spec").axes[0].label.formatMethod).toBe(formatter);
    expect(spec.color).toEqual(["var(--uvp-brand)"]);
    document.body.style.setProperty("--uvp-brand", "#60a5fa");
    await new Promise(resolve => setTimeout(resolve, 0));
    await nextTick();
    expect(chart.props("spec").color).toEqual(["#60a5fa"]);
    wrapper.unmount();
  });
});
