import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import FlowArrow from "./FlowArrow.vue";
import { resolveArrowColor, FLOW_ARROW_COLORS } from "./flow-arrow-colors";

describe("resolveArrowColor", () => {
    it("returns request gray when direction is request", () => {
        expect(resolveArrowColor("request", 200)).toBe(FLOW_ARROW_COLORS.request);
    });
    it("returns success green for 2xx response", () => {
        expect(resolveArrowColor("response", 200)).toBe(FLOW_ARROW_COLORS.success);
        expect(resolveArrowColor("response", 299)).toBe(FLOW_ARROW_COLORS.success);
    });
    it("returns redirect yellow for 3xx response", () => {
        expect(resolveArrowColor("response", 302)).toBe(FLOW_ARROW_COLORS.redirect);
    });
    it("returns failure red for 4xx-6xx response", () => {
        expect(resolveArrowColor("response", 404)).toBe(FLOW_ARROW_COLORS.failure);
        expect(resolveArrowColor("response", 503)).toBe(FLOW_ARROW_COLORS.failure);
        expect(resolveArrowColor("response", 603)).toBe(FLOW_ARROW_COLORS.failure);
    });
    it("returns request gray when statusCode undefined even on response", () => {
        expect(resolveArrowColor("response", undefined)).toBe(FLOW_ARROW_COLORS.request);
    });
});

describe("FlowArrow", () => {
    const base = { from: { x: 10, y: 20 }, to: { x: 100, y: 20 }, label: "INVITE", direction: "request" as const };

    it("renders line and polygon", () => {
        const wrapper = mount(FlowArrow, { props: base });
        expect(wrapper.find("line").exists()).toBe(true);
        expect(wrapper.find("polygon").exists()).toBe(true);
    });

    it("colors line by resolved color (200 response = green)", () => {
        const wrapper = mount(FlowArrow, { props: { ...base, direction: "response", statusCode: 200 } });
        expect(wrapper.find("line").attributes("stroke")).toBe(FLOW_ARROW_COLORS.success);
    });

    it("colors line red for 404", () => {
        const wrapper = mount(FlowArrow, { props: { ...base, direction: "response", statusCode: 404 } });
        expect(wrapper.find("line").attributes("stroke")).toBe(FLOW_ARROW_COLORS.failure);
    });

    it("uses gray when direction=request even with statusCode", () => {
        const wrapper = mount(FlowArrow, { props: { ...base, statusCode: 500 } });
        expect(wrapper.find("line").attributes("stroke")).toBe(FLOW_ARROW_COLORS.request);
    });

    it("renders label text", () => {
        const wrapper = mount(FlowArrow, { props: base });
        expect(wrapper.text()).toContain("INVITE");
    });
});
