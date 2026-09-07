import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  router: { replace: vi.fn() },
  refreshPhase: vi.fn(),
  store: { state: null as any, openModal: vi.fn() }
}));

vi.mock("vue-router", () => ({ useRouter: () => mocks.router }));
vi.mock("@/api/standalone-setup", () => ({ refreshStandaloneSetupStatus: mocks.refreshPhase }));
vi.mock("@/layout/components/SipSetupHost.vue", () => ({
  default: { name: "SipSetupHost", template: '<div data-testid="sip-setup-host" />' }
}));
vi.mock("@/store/modules/sip-setup", async () => {
  const { reactive } = await vi.importActual<typeof import("vue")>("vue");
  mocks.store.state = reactive({ modalOpen: false, openModal: mocks.store.openModal });
  return { useSipSetupStore: () => mocks.store.state };
});

import StandaloneSIPSetup from "./index.vue";

describe("standalone SIP setup page", () => {
  beforeEach(() => {
    mocks.router.replace.mockReset();
    mocks.refreshPhase.mockReset();
    mocks.store.openModal.mockReset();
    mocks.store.state.modalOpen = false;
  });

  it("renders the SIP host in a standalone page without the application layout", () => {
    const wrapper = mount(StandaloneSIPSetup);

    expect(wrapper.find("[data-testid='standalone-sip-setup-page']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='sip-setup-host']").exists()).toBe(true);
    expect(wrapper.find(".layout").exists()).toBe(false);
    wrapper.unmount();
  });

  it("refreshes the installation phase and enters home after SIP completion", async () => {
    mocks.refreshPhase.mockResolvedValue({ kind: "standalone", status: { standalone: true, phase: "complete" } });
    const wrapper = mount(StandaloneSIPSetup);

    mocks.store.state.modalOpen = true;
    await flushPromises();
    mocks.store.state.modalOpen = false;
    await flushPromises();

    expect(mocks.refreshPhase).toHaveBeenCalledTimes(1);
    expect(mocks.router.replace).toHaveBeenCalledWith("/home");
    wrapper.unmount();
  });

  it("reopens the required host when the phase is still pending SIP", async () => {
    mocks.refreshPhase.mockResolvedValue({ kind: "standalone", status: { standalone: true, phase: "pending_sip" } });
    const wrapper = mount(StandaloneSIPSetup);

    mocks.store.state.modalOpen = true;
    await flushPromises();
    mocks.store.state.modalOpen = false;
    await flushPromises();

    expect(mocks.store.openModal).toHaveBeenCalledTimes(1);
    expect(mocks.router.replace).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
