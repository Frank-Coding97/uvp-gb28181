import { flushPromises, mount } from "@vue/test-utils";
import { Modal } from "@arco-design/web-vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SipSetupModal from "./SipSetupModal.vue";

const mocks = vi.hoisted(() => ({
  form: {
    deploymentMode: "public",
    listenIp: "192.168.1.10",
    advertiseIp: "203.0.113.10",
    hookIp: "192.0.2.11",
    sdpIp: "192.0.2.12",
    streamIp: "192.0.2.13",
    port: 5061,
    domain: "3402000000",
    serverId: "34020000002000000001",
    password: ""
  },
  save: vi.fn(),
  loadStatus: vi.fn(),
  loadNetwork: vi.fn()
}));

vi.mock("@/views/gb28181/sip/useSipSetup", async () => {
  const vue = await import("vue");
  return {
    useSipSetup: () => ({
      form: vue.reactive(mocks.form),
      saving: vue.ref(false),
      status: vue.ref(null),
      network: vue.ref(null),
      hasExistingPassword: vue.ref(true),
      error: vue.ref(""),
      save: mocks.save,
      loadStatus: mocks.loadStatus,
      loadNetwork: mocks.loadNetwork
    })
  };
});

const ModalStub = {
  props: ["visible"],
  template: '<div v-if="visible"><slot /><slot name="footer" /></div>'
};

const ButtonStub = {
  props: ["disabled", "loading"],
  emits: ["click"],
  template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot name="icon" /><slot /></button>'
};

function mountModal(editing: boolean) {
  return mount(SipSetupModal, {
    props: { visible: true, editing },
    global: { stubs: { "a-modal": ModalStub, "a-button": ButtonStub } }
  });
}

async function advanceToConfirmation(wrapper: ReturnType<typeof mountModal>) {
  for (let i = 0; i < 3; i++) {
    await wrapper
      .findAll("button")
      .find(button => button.text().includes("下一步"))!
      .trigger("click");
  }
}

describe("SipSetupModal SIP reload confirmation", () => {
  beforeEach(() => {
    mocks.save.mockReset().mockResolvedValue({ reloadedOk: true, mediaDefaultsApplied: true });
    mocks.loadStatus.mockReset().mockResolvedValue(undefined);
    mocks.loadNetwork.mockReset().mockResolvedValue(undefined);
    vi.spyOn(Modal, "confirm").mockReset();
  });

  it("warns before applying edits and saves only after confirmation", async () => {
    const wrapper = mountModal(true);
    await advanceToConfirmation(wrapper);
    await wrapper
      .findAll("button")
      .find(button => button.text().includes("保存并应用"))!
      .trigger("click");

    expect(Modal.confirm).toHaveBeenCalledOnce();
    expect(mocks.save).not.toHaveBeenCalled();
    const confirmation = vi.mocked(Modal.confirm).mock.calls[0][0];
    expect(confirmation.content).toContain("SIP 信令会短暂中断");
    expect(confirmation.content).toContain("点播、回放、对讲");
    expect(confirmation.content).toContain("不会重启");

    await confirmation.onOk?.();
    await flushPromises();
    expect(mocks.save).toHaveBeenCalledOnce();
  });

  it("starts the initial setup without a reload warning", async () => {
    const wrapper = mountModal(false);
    await advanceToConfirmation(wrapper);
    await wrapper
      .findAll("button")
      .find(button => button.text().includes("保存并启动"))!
      .trigger("click");
    await flushPromises();

    expect(Modal.confirm).not.toHaveBeenCalled();
    expect(mocks.save).toHaveBeenCalledOnce();
  });
});
