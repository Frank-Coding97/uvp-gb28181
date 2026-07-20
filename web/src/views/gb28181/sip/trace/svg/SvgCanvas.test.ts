import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import { h, inject, defineComponent } from "vue";
import SvgCanvas from "./SvgCanvas.vue";
import { SVG_CANVAS_HOVER_KEY } from "./svg-canvas-context";

describe("SvgCanvas", () => {
    it("renders svg with viewBox attribute", () => {
        const wrapper = mount(SvgCanvas, {
            props: { viewBox: "0 0 100 50" }
        });
        const svg = wrapper.find("svg");
        expect(svg.exists()).toBe(true);
        expect(svg.attributes("viewBox")).toBe("0 0 100 50");
    });

    it("renders slot content inside svg", () => {
        const wrapper = mount(SvgCanvas, {
            props: { viewBox: "0 0 100 50" },
            slots: {
                default: () => h("circle", { cx: 10, cy: 10, r: 5 })
            }
        });
        expect(wrapper.find("svg circle").exists()).toBe(true);
    });

    it("provides hover coords updated on mousemove", async () => {
        const Child = defineComponent({
            setup() {
                const hover = inject(SVG_CANVAS_HOVER_KEY);
                return () =>
                    h("text", { class: "hover-probe" }, hover?.value ? `${hover.value.x},${hover.value.y}` : "null");
            }
        });
        const wrapper = mount(SvgCanvas, {
            props: { viewBox: "0 0 100 50" },
            slots: { default: () => h(Child) }
        });
        expect(wrapper.find(".hover-probe").text()).toBe("null");

        const svg = wrapper.find("svg").element as SVGSVGElement;
        // Stub getBoundingClientRect: 200x100 real → viewBox 100x50 → scale 2x2
        Object.defineProperty(svg, "getBoundingClientRect", {
            value: () => ({ left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100, x: 0, y: 0, toJSON: () => ({}) }),
            configurable: true
        });
        await wrapper.find("svg").trigger("mousemove", { clientX: 40, clientY: 20 });
        expect(wrapper.find(".hover-probe").text()).toBe("20,10");

        await wrapper.find("svg").trigger("mouseleave");
        expect(wrapper.find(".hover-probe").text()).toBe("null");
    });
});
