import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import SpikeDemo from "./SpikeDemo.vue";

describe("SpikeDemo (T-1.3 mount cost)", () => {
    it("mounts 250 flow arrows within reasonable time (happy-dom, no repaint)", () => {
        const start = performance.now();
        const wrapper = mount(SpikeDemo);
        const elapsed = performance.now() - start;

        // 5 rows * 50 arrows = 250 FlowArrow instances = 250 <g> groups
        const groups = wrapper.findAll(".flow-arrow");
        expect(groups.length).toBe(250);

        // happy-dom baseline: 250 element mount should sit well under 500ms
        // (real browser will be faster; this is a floor sanity check, not fps proof)
        expect(elapsed).toBeLessThan(2000);
    });
});
