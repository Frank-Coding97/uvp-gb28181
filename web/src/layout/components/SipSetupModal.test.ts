import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const setupMock = vi.hoisted(() => ({
  saving: { value: false },
  error: { value: "" },
  status: { value: null as { config?: { deploymentMode: "lan" | "public"; listenIp: string; advertiseIp: string } } | null },
  form: {
    deploymentMode: "lan",
    listenIp: "192.168.1.10",
    advertiseIp: "192.168.1.10",
    advertiseIpInferred: false,
    mediaReceiveHost: "192.168.1.10",
    mediaPlaybackHost: "192.168.1.10",
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: ""
  },
  network: {
    value: {
      items: [] as Array<{
        ip: string;
        cidr: string;
        loopback: boolean;
        virtual: boolean;
        recommended: boolean;
        more: boolean;
        listenOnly: boolean;
      }>,
      scanStatus: "ok" as "ok" | "failed"
    }
  },
  hasExistingPassword: { value: false },
  loadStatus: vi.fn(),
  loadNetwork: vi.fn(),
  save: vi.fn()
}));
const standaloneStatusLoader = vi.hoisted(() => vi.fn());

vi.mock("@/views/gb28181/sip/steps/DeploymentStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/steps/NetworkStep.vue", () => ({
  default: {
    props: ["form", "network"],
    computed: {
      ips() {
        const component = this as unknown as { network?: { items?: Array<{ ip: string }> } };
        return (component.network?.items || []).map(item => item.ip).join(",");
      }
    },
    template: '<div data-testid="network-step" :data-listen-ip="form.listenIp" :data-network-ips="ips" />'
  }
}));
vi.mock("@/views/gb28181/sip/steps/IdentityStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/steps/ConfirmStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/useSipSetup", () => ({ useSipSetup: () => setupMock }));
vi.mock("@arco-design/web-vue", () => ({ Message: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }));
vi.mock("@/api/standalone-setup", () => ({ loadStandaloneSetupStatus: standaloneStatusLoader }));

import SipSetupModal from "./SipSetupModal.vue";

const modalStub = {
  name: "ModalStub",
  props: {
    visible: Boolean,
    closable: Boolean,
    maskClosable: Boolean,
    escToClose: Boolean
  },
  emits: ["cancel"],
  template:
    '<section v-if="visible" data-testid="sip-modal" :data-closable="String(closable)" :data-mask-closable="String(maskClosable)" :data-esc-to-close="String(escToClose)"><slot /><slot name="footer" /></section>'
};
const buttonStub = {
  props: { disabled: Boolean, loading: Boolean },
  template: '<button :disabled="disabled"><slot name="icon" /><slot /></button>'
};

function mountModal(editing = false, required = false, standalone = false) {
  return mount(SipSetupModal, {
    props: { visible: true, editing, required, standalone },
    global: {
      stubs: {
        "a-modal": modalStub,
        "a-button": buttonStub,
        "a-steps": { template: "<div><slot /></div>" },
        "a-step": { template: "<div />" }
      }
    }
  });
}

async function openModal(wrapper: ReturnType<typeof mount>) {
  await wrapper.setProps({ visible: false });
  await wrapper.setProps({ visible: true });
  await flushPromises();
}

describe("SIP setup modal", () => {
  beforeEach(() => {
    setupMock.status.value = null;
    setupMock.network.value = { items: [], scanStatus: "ok" };
    Object.assign(setupMock.form, {
      deploymentMode: "lan",
      listenIp: "192.168.1.10",
      advertiseIp: "192.168.1.10",
      mediaReceiveHost: "192.168.1.10",
      mediaPlaybackHost: "192.168.1.10",
      password: ""
    });
    standaloneStatusLoader.mockReset().mockResolvedValue({ kind: "legacy" });
    vi.clearAllMocks();
  });

  it("keeps skip and close for legacy onboarding", async () => {
    const wrapper = mountModal();

    expect(wrapper.find(".sip-modal-skip").text()).toBe("稍后再配");

    await wrapper.find(".sip-modal-skip").trigger("click");
    expect(wrapper.emitted("close")).toHaveLength(1);
  });

  it("does not allow skipping or closing pending SIP onboarding", async () => {
    const wrapper = mountModal(false, true);
    const modal = wrapper.findComponent(modalStub);

    expect(modal.attributes("data-closable")).toBe("false");
    expect(modal.attributes("data-mask-closable")).toBe("false");
    expect(modal.attributes("data-esc-to-close")).toBe("false");
    expect(wrapper.find(".sip-modal-skip").exists()).toBe(false);
    expect(wrapper.text()).toContain("请完成配置后再使用系统");
    expect(wrapper.text()).not.toContain("稍后再配");

    await modal.vm.$emit("cancel");
    expect(wrapper.emitted("close")).toBeUndefined();
  });

  it("keeps the cancel action for editable SIP configuration", async () => {
    const wrapper = mountModal(true);
    const modal = wrapper.findComponent(modalStub);

    expect(modal.attributes("data-closable")).toBe("true");
    expect(wrapper.find(".sip-modal-skip").text()).toBe("取消");

    await modal.vm.$emit("cancel");
    expect(wrapper.emitted("close")).toHaveLength(1);
  });

  it("does not warn when the saved standalone address is still present", async () => {
    setupMock.status.value = {
      config: { deploymentMode: "lan", listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" }
    };
    setupMock.network.value = {
      items: [
        {
          ip: "192.168.1.10",
          cidr: "192.168.1.10/24",
          loopback: false,
          virtual: false,
          recommended: true,
          more: false,
          listenOnly: false
        }
      ],
      scanStatus: "ok"
    };
    const wrapper = mountModal(false, false, true);
    await openModal(wrapper);

    expect(wrapper.find(".sip-modal-address-warning").exists()).toBe(false);
  });

  it("warns about a missing standalone address and keeps it in the form options", async () => {
    setupMock.status.value = {
      config: { deploymentMode: "lan", listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" }
    };
    setupMock.network.value = {
      items: [
        {
          ip: "192.168.1.20",
          cidr: "192.168.1.20/24",
          loopback: false,
          virtual: false,
          recommended: true,
          more: false,
          listenOnly: false
        }
      ],
      scanStatus: "ok"
    };
    const wrapper = mountModal(false, false, true);
    await openModal(wrapper);

    expect(wrapper.find(".sip-modal-address-warning").text()).toContain("已保存的本机 SIP 地址不在当前网卡列表中");
    const next = wrapper.findAll("button").find(button => button.text() === "下一步");
    await next?.trigger("click");
    expect(wrapper.get("[data-testid='network-step']").attributes("data-listen-ip")).toBe("192.168.1.10");
    expect(wrapper.get("[data-testid='network-step']").attributes("data-network-ips")).toContain("192.168.1.10");
  });

  it("does not report a standalone address change when scanning fails", async () => {
    setupMock.status.value = {
      config: { deploymentMode: "lan", listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" }
    };
    setupMock.network.value = { items: [], scanStatus: "failed" };
    const wrapper = mountModal(false, false, true);
    await openModal(wrapper);

    expect(wrapper.find(".sip-modal-address-warning").exists()).toBe(false);
  });

  it("detects standalone mode from the cached probe for the edit entry", async () => {
    standaloneStatusLoader.mockResolvedValue({
      kind: "standalone",
      status: { phase: "complete", standalone: true }
    });
    setupMock.status.value = {
      config: { deploymentMode: "lan", listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" }
    };
    setupMock.network.value = {
      items: [
        {
          ip: "192.168.1.20",
          cidr: "192.168.1.20/24",
          loopback: false,
          virtual: false,
          recommended: true,
          more: false,
          listenOnly: false
        }
      ],
      scanStatus: "ok"
    };
    const wrapper = mountModal(true);
    await openModal(wrapper);

    expect(wrapper.find(".sip-modal-address-warning").exists()).toBe(true);
  });

  it("keeps the address reminder disabled for legacy setup", async () => {
    setupMock.status.value = {
      config: { deploymentMode: "lan", listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" }
    };
    setupMock.network.value = { items: [], scanStatus: "ok" };
    const wrapper = mountModal();
    await openModal(wrapper);

    expect(wrapper.find(".sip-modal-address-warning").exists()).toBe(false);
  });

  it("opts into media hosts only for required standalone onboarding", async () => {
    setupMock.form.password = "Sec12345Aa!!";
    setupMock.save.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        config: {},
        reloadedOk: true,
        reloadError: "",
        runtime: { state: "running", updatedAt: "" }
      }
    });
    const wrapper = mountModal(false, true, true);
    await openModal(wrapper);

    for (let step = 1; step < 4; step++) {
      const next = wrapper.findAll("button").find(button => button.text() === "下一步");
      await next?.trigger("click");
    }
    await wrapper.findAll("button").find(button => button.text() === "保存并启动")?.trigger("click");

    expect(setupMock.save).toHaveBeenCalledWith({ includeMediaHosts: true });
  });

  it("does not opt into media hosts for legacy onboarding", async () => {
    setupMock.form.password = "Sec12345Aa!!";
    setupMock.save.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        config: {},
        reloadedOk: true,
        reloadError: "",
        runtime: { state: "running", updatedAt: "" }
      }
    });
    const wrapper = mountModal();
    await openModal(wrapper);

    for (let step = 1; step < 4; step++) {
      const next = wrapper.findAll("button").find(button => button.text() === "下一步");
      await next?.trigger("click");
    }
    await wrapper.findAll("button").find(button => button.text() === "保存并启动")?.trigger("click");

    expect(setupMock.save).toHaveBeenCalledWith({ includeMediaHosts: false });
  });
});
