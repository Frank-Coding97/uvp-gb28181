import { mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it } from "vitest";
import IdentityStep from "./IdentityStep.vue";

const form = {
  deploymentMode: "lan" as const,
  listenIp: "0.0.0.0",
  advertiseIp: "",
  advertiseIpInferred: false,
  hookIp: "",
  streamIp: "",
  port: 5062,
  serverId: "34020000002000000002",
  domain: "4401000000",
  password: ""
};

describe("IdentityStep domain editing", () => {
  it("preserves a custom domain on mount and when the platform ID changes", async () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.emitted("update")).toBeUndefined();
    const inputs = wrapper.findAll("input");
    expect(inputs[2].attributes("disabled")).toBeUndefined();
    await inputs[2].setValue("1101000000");
    expect(wrapper.emitted("update")?.at(-1)).toEqual([{ domain: "1101000000" }]);
    await inputs[1].setValue("51010000002000000001");
    expect(wrapper.emitted("update")?.at(-1)).toEqual([{ serverId: "51010000002000000001" }]);
    await wrapper.setProps({ form: { ...form, serverId: "51010000002000000001" } });
    expect(wrapper.emitted("update")).toHaveLength(2);
  });

  it("initializes an empty domain from the platform ID", () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form, domain: "" }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.emitted("update")?.[0]).toEqual([{ domain: "3402000000" }]);
  });
});
