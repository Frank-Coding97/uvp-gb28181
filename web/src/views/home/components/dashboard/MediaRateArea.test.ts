import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MediaRateArea from "./MediaRateArea.vue";

describe("MediaRateArea", () => {
  it("renders a smooth realtime line with a matching gradient area", () => {
    const wrapper = mount(MediaRateArea, { props: { values: [1, 4, 2, 5], color: "#2563eb" } });
    const line = wrapper.get("path.media-rate-area__line");
    const area = wrapper.get("path.media-rate-area__fill");
    const gradientId = wrapper.get("linearGradient").attributes("id");

    expect(line.attributes("d")).toContain(" C ");
    expect(line.attributes("stroke")).toBe("#2563eb");
    expect(area.attributes("d")).toContain("L 100 36 L 0 36 Z");
    expect(area.attributes("fill")).toBe(`url(#${gradientId})`);
    expect(wrapper.get(".media-rate-area__y-axis").text()).toContain("B/s");
    expect(wrapper.get(".media-rate-area__x-axis").text()).toContain("2 分钟前");
    expect(wrapper.get(".media-rate-area__x-axis").text()).toContain("1 分钟前");
    expect(wrapper.text()).toContain("现在");
  });

  it("renders an empty realtime series on the baseline", () => {
    const wrapper = mount(MediaRateArea, { props: { values: [] } });
    expect(wrapper.get("path.media-rate-area__line").attributes("d")).toContain("M 0 32");
    expect(wrapper.get(".media-rate-area__y-axis").text()).toContain("--");
    expect(wrapper.get(".media-rate-area__y-axis").text()).toContain("0 B/s");
  });
});
