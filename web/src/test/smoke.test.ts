import { mount } from "@vue/test-utils";
import { defineComponent } from "vue";
import { describe, expect, it } from "vitest";

describe("Vue unit test harness", () => {
    it("mounts a component with the shared Arco button stub", () => {
        const component = defineComponent({ template: "<a-button>保存</a-button>" });
        const wrapper = mount(component);
        expect(wrapper.get("button").text()).toBe("保存");
    });
});
