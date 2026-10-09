import { mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it } from "vitest";
import type { SipSetupForm } from "../useSipSetup";
import NetworkStep from "./NetworkStep.vue";

const SDP_PLACEHOLDER = 'input[placeholder="设备可访问的 IP 或域名"]';
const HOOK_PLACEHOLDER = 'input[placeholder="具体 IPv4 或 IPv6"]';
const STREAM_PLACEHOLDER = 'input[placeholder="播放地址（可留空）"]';

function createForm(deploymentMode: "lan" | "public"): SipSetupForm {
  return {
    deploymentMode,
    listenIp: "192.168.1.10",
    advertiseIp: deploymentMode === "public" ? "203.0.113.10" : "192.168.1.10",
    advertiseIpInferred: false,
    hookIp: "",
    // SDP IP 是必填项，夹具给一个合法值，避免每条用例都被必填校验挡住。
    sdpIp: "192.168.1.10",
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
    expect(wrapper.find(HOOK_PLACEHOLDER).exists()).toBe(true);
    expect(wrapper.find(SDP_PLACEHOLDER).exists()).toBe(true);
    expect(wrapper.find(STREAM_PLACEHOLDER).exists()).toBe(true);
    wrapper.unmount();
  });

  it("emits hookIp, sdpIp and streamIp patches without changing the main network fields", async () => {
    const wrapper = mount(NetworkStep, {
      props: { form: createForm("lan"), network: null },
      global: { plugins: [ArcoVue] }
    });

    await wrapper.find(HOOK_PLACEHOLDER).setValue("2001:db8::10");
    await wrapper.find(SDP_PLACEHOLDER).setValue("media.example.com");
    await wrapper.find(STREAM_PLACEHOLDER).setValue("play.example.com");
    expect(wrapper.emitted("update")).toEqual([
      [{ hookIp: "2001:db8::10" }],
      [{ sdpIp: "media.example.com" }],
      [{ streamIp: "play.example.com" }]
    ]);
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

  // SDP IP 必填，且必须拒绝回环地址 —— 这正是「注册成功但拉不到流」的真凶。
  it("rejects a loopback SDP IP and explains the consequence", () => {
    const wrapper = mount(NetworkStep, {
      props: { form: { ...createForm("lan"), sdpIp: "127.0.0.1" }, network: null },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.text()).toContain("注册成功但没有画面");
    wrapper.unmount();
  });

  it("requires the SDP IP to be filled in", () => {
    const wrapper = mount(NetworkStep, {
      props: { form: { ...createForm("lan"), sdpIp: "" }, network: null },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.text()).toContain("设备按此地址把视频流推回平台");
    wrapper.unmount();
  });

  it("rejects a container-name style SDP IP", () => {
    const wrapper = mount(NetworkStep, {
      props: { form: { ...createForm("lan"), sdpIp: "polaris-media" }, network: null },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.text()).toContain("单一主机名");
    wrapper.unmount();
  });
});
