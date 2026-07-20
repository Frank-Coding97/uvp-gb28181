import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import DeploymentStep from "./DeploymentStep.vue";

describe("DeploymentStep", () => {
    it("selects LAN or public deployment", async () => {
        const wrapper = mount(DeploymentStep, { props: { modelValue: "" } });
        const options = wrapper.findAll("button");
        expect(options).toHaveLength(2);
        await options[0].trigger("click");
        expect(wrapper.emitted("update:modelValue")?.[0]).toEqual(["lan"]);
        await options[1].trigger("click");
        expect(wrapper.emitted("update:modelValue")?.[1]).toEqual(["public"]);
    });
});
