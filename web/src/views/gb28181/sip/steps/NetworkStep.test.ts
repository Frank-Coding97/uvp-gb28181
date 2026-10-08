import { mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it } from "vitest";
import type { SipSetupForm } from "../useSipSetup";
import NetworkStep from "./NetworkStep.vue";

function createForm(deploymentMode: "lan" | "public"): SipSetupForm {
  return {
    deploymentMode,
    listenIp: "192.168.1.10",
    advertiseIp: deploymentMode === "public" ? "203.0.113.10" : "192.168.1.10",
    advertiseIpInferred: false,
    hookIp: "",
    streamIp: "",
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: ""
  };
}

describe("NetworkStep media addresses", () => {
  it.each(["lan", "public"] as const)("shows optional media addresses in %s mode", deploymentMode => {
    const wrapper = mount(NetworkStep, {
      props: { form: createForm(deploymentMode), network: null },
      global: { plugins: [ArcoVue] }
    });

    expect(wrapper.text()).toContain("媒体网络地址");
    expect(wrapper.text()).toContain("Hook IP");
    expect(wrapper.text()).toContain("Stream IP");
    expect(wrapper.find('input[placeholder="具体 IPv4 或 IPv6"]').exists()).toBe(true);
    expect(wrapper.find('input[placeholder="IP 地址或域名"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("emits hookIp and streamIp field patches without changing the main network fields", async () => {
    const wrapper = mount(NetworkStep, {
      props: { form: createForm("lan"), network: null },
      global: { plugins: [ArcoVue] }
    });

    await wrapper.find('input[placeholder="具体 IPv4 或 IPv6"]').setValue("2001:db8::10");
    await wrapper.find('input[placeholder="IP 地址或域名"]').setValue("media.example.com");
    expect(wrapper.emitted("update")).toEqual([[{ hookIp: "2001:db8::10" }], [{ streamIp: "media.example.com" }]]);
    wrapper.unmount();
  });

  it("shows inline validation errors for invalid optional media values", () => {
    const wrapper = mount(NetworkStep, {
      props: { form: { ...createForm("public"), hookIp: "0.0.0.0", streamIp: "https://media.example.com/live" }, network: null },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.text()).toContain("具体的 IPv4 或 IPv6");
    expect(wrapper.text()).toContain("不含协议、端口和路径");
    wrapper.unmount();
  });
});
