import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

const setupMock = vi.hoisted(() => ({
  saving: { value: false },
  error: { value: "" },
  form: {
    deploymentMode: "lan",
    listenIp: "192.168.1.10",
    advertiseIp: "192.168.1.10",
    advertiseIpInferred: false,
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: ""
  },
  network: { value: { items: [], scanStatus: "ok" } },
  hasExistingPassword: { value: false },
  loadStatus: vi.fn(),
  loadNetwork: vi.fn(),
  save: vi.fn()
}));

vi.mock("@/views/gb28181/sip/steps/DeploymentStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/steps/NetworkStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/steps/IdentityStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/steps/ConfirmStep.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/views/gb28181/sip/useSipSetup", () => ({ useSipSetup: () => setupMock }));
vi.mock("@arco-design/web-vue", () => ({ Message: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }));

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

function mountModal(editing = false, required = false) {
  return mount(SipSetupModal, {
    props: { visible: true, editing, required },
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

describe("SIP setup modal", () => {
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
});
