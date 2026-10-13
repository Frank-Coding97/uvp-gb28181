import { mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it } from "vitest";
import type { SipNetworkAddress } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import NetworkStep from "./NetworkStep.vue";

const HOOK_INPUT = '[data-testid="media-hook-ip"]';
const SDP_INPUT = '[data-testid="media-sdp-ip"]';
const STREAM_INPUT = '[data-testid="media-stream-ip"]';

function createForm(deploymentMode: "lan" | "public"): SipSetupForm {
  return {
    deploymentMode,
    listenIp: "192.168.1.10",
    advertiseIp: deploymentMode === "public" ? "203.0.113.10" : "192.168.1.10",
    advertiseIpInferred: false,
    hookIp: "",
    sdpIp: "",
    streamIp: "",
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: ""
  };
}

/** 选中具体真实网卡时的网卡列表（夹具 listenIp 落在它上面）。 */
const physicalNic: SipNetworkAddress = {
  ip: "192.168.1.10",
  cidr: "192.168.1.10/24",
  loopback: false,
  virtual: false,
  recommended: true,
  more: true,
  listenOnly: false
};

function mountStep(form: SipSetupForm, items: SipNetworkAddress[] | null = [physicalNic]) {
  return mount(NetworkStep, {
    props: { form, network: items ? { items, scanStatus: "ok" as const } : null },
    global: { plugins: [ArcoVue] }
  });
}

function placeholderOf(wrapper: ReturnType<typeof mountStep>, selector: string): string {
  return wrapper.find(selector).attributes("placeholder") || "";
}

describe("NetworkStep media addresses", () => {
  it.each(["lan", "public"] as const)("shows the media address fields in %s mode", deploymentMode => {
    const wrapper = mountStep(createForm(deploymentMode));

    expect(wrapper.text()).toContain("媒体网络地址");
    expect(wrapper.text()).toContain("Hook IP");
    expect(wrapper.text()).toContain("SDP IP");
    expect(wrapper.text()).toContain("Stream IP");
    expect(wrapper.find(HOOK_INPUT).exists()).toBe(true);
    expect(wrapper.find(SDP_INPUT).exists()).toBe(true);
    expect(wrapper.find(STREAM_INPUT).exists()).toBe(true);
    wrapper.unmount();
  });

  // 老板要的规则①：选中真实网卡 ⇒ 三项可留空，placeholder 要写明「留空会用哪个地址」。
  it("tells the operator which address will be used when the field is left blank", () => {
    const wrapper = mountStep(createForm("lan"));

    for (const selector of [HOOK_INPUT, SDP_INPUT, STREAM_INPUT]) {
      expect(placeholderOf(wrapper, selector)).toContain("可留空");
      expect(placeholderOf(wrapper, selector)).toContain("192.168.1.10");
    }
    expect(wrapper.text()).toContain("留空时自动使用 192.168.1.10");
    wrapper.unmount();
  });

  // 老板要的规则②：虚拟网卡地址设备侧不可达 ⇒ 三项必须手填，并说明原因。
  it("requires every media address when the selected NIC is virtual", () => {
    const wrapper = mountStep(createForm("lan"), [
      { ...physicalNic, interfaceName: "VMware Network Adapter VMnet1", virtual: true }
    ]);

    for (const selector of [HOOK_INPUT, SDP_INPUT, STREAM_INPUT]) {
      expect(placeholderOf(wrapper, selector)).toContain("必填");
    }
    expect(wrapper.text()).toContain("虚拟网卡");
    wrapper.unmount();
  });

  // 老板要的规则②：0.0.0.0 推断不出唯一地址 ⇒ 三项必须手填。
  it("requires every media address for the wildcard listen address", () => {
    const wrapper = mountStep({ ...createForm("lan"), listenIp: "0.0.0.0" });

    for (const selector of [HOOK_INPUT, SDP_INPUT, STREAM_INPUT]) {
      expect(placeholderOf(wrapper, selector)).toContain("必填");
    }
    expect(wrapper.text()).toContain("0.0.0.0");
    wrapper.unmount();
  });

  // 老板要的规则③：公网模式下 SDP / Stream 跟随公网接入 IP，Hook 仍走监听网卡。
  it("splits the public-mode fallbacks between the public IP and the listen NIC", () => {
    const wrapper = mountStep({ ...createForm("public"), listenIp: "10.0.8.3" }, [
      { ...physicalNic, ip: "10.0.8.3", cidr: "10.0.8.3/24" }
    ]);

    expect(placeholderOf(wrapper, HOOK_INPUT)).toContain("10.0.8.3");
    expect(placeholderOf(wrapper, SDP_INPUT)).toContain("203.0.113.10");
    expect(placeholderOf(wrapper, STREAM_INPUT)).toContain("203.0.113.10");
    wrapper.unmount();
  });

  it("emits hookIp, sdpIp and streamIp patches without changing the main network fields", async () => {
    const wrapper = mountStep(createForm("lan"));

    await wrapper.find(HOOK_INPUT).setValue("2001:db8::10");
    await wrapper.find(SDP_INPUT).setValue("media.example.com");
    await wrapper.find(STREAM_INPUT).setValue("play.example.com");
    expect(wrapper.emitted("update")).toEqual([
      [{ hookIp: "2001:db8::10" }],
      [{ sdpIp: "media.example.com" }],
      [{ streamIp: "play.example.com" }]
    ]);
    wrapper.unmount();
  });

  it("shows inline validation errors for invalid media values", () => {
    const wrapper = mountStep({ ...createForm("public"), hookIp: "0.0.0.0", streamIp: "https://media.example.com/live" });
    expect(wrapper.text()).toContain("具体的 IPv4 或 IPv6");
    expect(wrapper.text()).toContain("不含协议、端口和路径");
    wrapper.unmount();
  });

  // 填了值就必须可用：回环地址正是「注册成功但拉不到流」的真凶。
  it("rejects a loopback SDP IP and explains the consequence", () => {
    const wrapper = mountStep({ ...createForm("lan"), sdpIp: "127.0.0.1" });
    expect(wrapper.text()).toContain("注册成功但没有画面");
    wrapper.unmount();
  });

  it("rejects a container-name style SDP IP", () => {
    const wrapper = mountStep({ ...createForm("lan"), sdpIp: "polaris-media" });
    expect(wrapper.text()).toContain("单一主机名");
    wrapper.unmount();
  });
});

function nic(overrides: Partial<SipNetworkAddress>): SipNetworkAddress {
  return {
    ip: "10.0.0.1",
    cidr: "10.0.0.1/24",
    loopback: false,
    virtual: false,
    recommended: false,
    more: true,
    listenOnly: false,
    ...overrides
  };
}

function mountWithNics(items: SipNetworkAddress[]) {
  return mount(NetworkStep, {
    props: {
      // 夹具里的 listenIp 不在网卡列表里，watcher 会立刻走"重选默认网卡"分支。
      form: createForm("lan"),
      network: { items, scanStatus: "ok" as const }
    },
    global: { plugins: [ArcoVue] }
  });
}

describe("NetworkStep default NIC selection", () => {
  // 后端判定靠网卡名猜，认不出某些虚拟网卡是常态（历史上 Windows 友好名整批漏过）。
  // 那种情况下后端不会打 recommended 标记，必须由前端兜底挑物理网卡，
  // 否则会按 IP 升序把虚拟网卡选成默认值 —— 这正是老板要修的问题。
  it("falls back to a physical NIC when the backend flagged nothing as recommended", () => {
    const wrapper = mountWithNics([
      nic({ ip: "10.0.0.1", interfaceName: "VMware Network Adapter VMnet1", virtual: true }),
      nic({ ip: "192.168.10.52", interfaceName: "Ethernet" })
    ]);

    expect(wrapper.emitted("update")).toEqual([
      [{ listenIp: "192.168.10.52", advertiseIp: "192.168.10.52", advertiseIpInferred: false }]
    ]);
    wrapper.unmount();
  });

  // 即使后端**错误地**把虚拟网卡标成推荐（前后端版本不同步也会这样），
  // 前端也不该跟着选它 —— "非虚拟"这一档的优先级高于"推荐"。
  it("ignores a recommended flag that points at a virtual NIC", () => {
    const wrapper = mountWithNics([
      nic({
        ip: "192.168.56.1",
        interfaceName: "VirtualBox Host-Only Network",
        virtual: true,
        recommended: true
      }),
      nic({ ip: "192.168.10.52", interfaceName: "Ethernet" })
    ]);

    expect(wrapper.emitted("update")?.[0]).toEqual([
      { listenIp: "192.168.10.52", advertiseIp: "192.168.10.52", advertiseIpInferred: false }
    ]);
    wrapper.unmount();
  });

  it("uses the backend recommended physical NIC when there is one", () => {
    const wrapper = mountWithNics([
      nic({ ip: "10.0.0.1", interfaceName: "Ethernet 2" }),
      nic({ ip: "192.168.10.52", interfaceName: "Ethernet", recommended: true, more: false })
    ]);

    expect(wrapper.emitted("update")?.[0]).toEqual([
      { listenIp: "192.168.10.52", advertiseIp: "192.168.10.52", advertiseIpInferred: false }
    ]);
    wrapper.unmount();
  });

  it("still picks something when every candidate is virtual", () => {
    const wrapper = mountWithNics([
      nic({ ip: "10.0.0.1", interfaceName: "br0", virtual: true }),
      nic({ ip: "172.20.0.1", interfaceName: "vEthernet (Default Switch)", virtual: true })
    ]);

    expect(wrapper.emitted("update")?.[0]).toEqual([
      { listenIp: "10.0.0.1", advertiseIp: "10.0.0.1", advertiseIpInferred: false }
    ]);
    wrapper.unmount();
  });
});
