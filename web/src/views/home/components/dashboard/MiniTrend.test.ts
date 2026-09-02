import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MiniTrend from "./MiniTrend.vue";

describe("MiniTrend", () => {
  it("renders a smooth line with a matching gradient area", () => {
    const wrapper = mount(MiniTrend, { props: { values: [1, 3, 2, 4], color: "#0faaa6" } });
    const line = wrapper.get("path.mini-trend__line");
    const area = wrapper.get("path.mini-trend__area");
    const gradientId = wrapper.get("linearGradient").attributes("id");

    expect(line.attributes("d")).toContain(" C ");
    expect(line.attributes("stroke")).toBe("#0faaa6");
    expect(area.attributes("d")).toContain("L 100 24 L 0 24 Z");
    expect(area.attributes("fill")).toBe(`url(#${gradientId})`);
    expect(wrapper.get("stop").attributes("stop-color")).toBe("#0faaa6");
  });

  it("keeps an empty real-data series on a flat baseline", () => {
    const wrapper = mount(MiniTrend, { props: { values: [], color: "#2563eb" } });
    expect(wrapper.get("path.mini-trend__line").attributes("d")).toContain("M 0 20");
  });
});
