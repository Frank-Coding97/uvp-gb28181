import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SipRuntimeState, SipSetupStatus } from "@/api/gb28181";
import { hasSipStatusPermission, hasSipUpdatePermission, shouldOpenSipSetup } from "./sipSetupHostRules";
import SipSetupHost from "./SipSetupHost.vue";
import { useSipSetupStore } from "@/store/modules/sip-setup";

const userAccount = vi.hoisted(() => ({ id: 0, permissions: [] as string[] }));
const standaloneStatusLoader = vi.hoisted(() => vi.fn());

vi.mock("@/store/modules/user", () => ({
    useUserStoreHook: () => ({ account: userAccount })
}));

vi.mock("@/api/standalone-setup", () => ({
    loadStandaloneSetupStatus: standaloneStatusLoader
}));

vi.mock("./SipSetupModal.vue", () => ({
    default: {
        name: "SipSetupModal",
        props: ["visible", "required"],
        template: '<div data-testid="sip-setup-modal" :data-visible="String(visible)" :data-required="String(required)" />'
    }
}));

// 2026-07-20 起 gating 只看 runtime.state,不再有 onboardingStatus 四态.
const status = (state: SipRuntimeState): SipSetupStatus => ({
    configStatus: state === "running" ? "configured" : "unconfigured",
    runtime: { state, updatedAt: "" }
});

describe("SipSetupHost rules", () => {
    it("opens on unconfigured or failed state for authorized user", () => {
        const permissions = ["gb28181:sip:config:update"];
        expect(shouldOpenSipSetup(status("unconfigured"), permissions)).toBe(true);
        expect(shouldOpenSipSetup(status("failed"), permissions)).toBe(true);
    });

    it("stays closed on running / starting / disabled", () => {
        const permissions = ["gb28181:sip:config:update"];
        expect(shouldOpenSipSetup(status("running"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("starting"), permissions)).toBe(false);
        expect(shouldOpenSipSetup(status("disabled"), permissions)).toBe(false);
    });

    it("respects permission gating", () => {
        expect(shouldOpenSipSetup(status("unconfigured"), [])).toBe(false);
        expect(shouldOpenSipSetup(status("unconfigured"), ["other:perm"])).toBe(false);
    });

    it("recognizes explicit and super administrator permissions", () => {
        expect(hasSipUpdatePermission([])).toBe(false);
        expect(hasSipUpdatePermission(["gb28181:sip:config:update"])).toBe(true);
        expect(hasSipUpdatePermission(["*:*:*"])).toBe(true);
    });

    it("only loads status for view, update, or wildcard permission", () => {
        expect(hasSipStatusPermission([])).toBe(false);
        expect(hasSipStatusPermission(["other:perm"])).toBe(false);
        expect(hasSipStatusPermission(["gb28181:sip:config:view"])).toBe(true);
        expect(hasSipStatusPermission(["gb28181:sip:config:update"])).toBe(true);
        expect(hasSipStatusPermission(["*:*:*"])).toBe(true);
    });
});

describe("SipSetupHost permission boundary", () => {
    let pinia: ReturnType<typeof createPinia>;

    beforeEach(() => {
        pinia = createPinia();
        setActivePinia(pinia);
        useSipSetupStore().reset();
        standaloneStatusLoader.mockReset().mockResolvedValue({ kind: "legacy" });
    });

    it("does not request status or mount the modal without SIP config access", async () => {
        const loader = vi.fn();
        const store = useSipSetupStore();
        store.openModal();
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: [], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await flushPromises();

        expect(loader).not.toHaveBeenCalled();
        expect(store.modalOpen).toBe(false);
        expect(wrapper.find("[data-testid='sip-setup-modal']").exists()).toBe(false);
        wrapper.unmount();
    });

    it("lets view permission load status while keeping the configuration modal closed", async () => {
        const loader = vi.fn().mockResolvedValue({ code: 0, data: status("unconfigured") });
        useSipSetupStore().openModal();
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: ["gb28181:sip:config:view"], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await flushPromises();

        expect(loader).toHaveBeenCalledTimes(1);
        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-visible")).toBe("false");
        wrapper.unmount();
    });

    it("lets update permission auto-open the configuration modal", async () => {
        const loader = vi.fn().mockResolvedValue({ code: 0, data: status("unconfigured") });
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: ["gb28181:sip:config:update"], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await flushPromises();

        expect(loader).toHaveBeenCalledTimes(1);
        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-visible")).toBe("true");
        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-required")).toBe("false");
        wrapper.unmount();
    });

    it("keeps legacy behavior when the standalone status is unavailable", async () => {
        standaloneStatusLoader.mockResolvedValue({ kind: "unavailable" });
        const loader = vi.fn().mockResolvedValue({ code: 0, data: status("unconfigured") });
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: ["gb28181:sip:config:update"], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await flushPromises();

        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-visible")).toBe("true");
        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-required")).toBe("false");
        wrapper.unmount();
    });

    it("forces pending standalone SIP onboarding despite session suppression", async () => {
        let resolveStatus: ((value: { code: number; data: SipSetupStatus }) => void) | undefined;
        const loader = vi.fn().mockImplementation(
            () => new Promise(resolve => {
                resolveStatus = resolve;
            })
        );
        standaloneStatusLoader.mockResolvedValue({
            kind: "standalone",
            status: { phase: "pending_sip", standalone: true }
        });
        const store = useSipSetupStore();
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: ["gb28181:sip:config:update"], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await flushPromises();
        store.closeModal({ suppressThisSession: true });
        resolveStatus?.({ code: 0, data: status("unconfigured") });
        await flushPromises();

        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-visible")).toBe("true");
        expect(wrapper.get("[data-testid='sip-setup-modal']").attributes("data-required")).toBe("true");
        wrapper.unmount();
    });

    it("ignores a late authorized response after permissions are removed", async () => {
        let resolveFirst: ((value: { code: number; data: SipSetupStatus }) => void) | undefined;
        const loader = vi.fn()
            .mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve; }))
            .mockResolvedValue({ code: 0, data: status("unconfigured") });
        const store = useSipSetupStore();
        store.openModal();
        const wrapper = mount(SipSetupHost, {
            props: { userId: 7, permissions: ["gb28181:sip:config:update"], statusLoader: loader },
            global: { plugins: [pinia] }
        });

        await wrapper.setProps({ permissions: [] });
        resolveFirst?.({ code: 0, data: status("unconfigured") });
        await flushPromises();

        expect(loader).toHaveBeenCalledTimes(1);
        expect(store.modalOpen).toBe(false);
        expect(wrapper.find("[data-testid='sip-setup-modal']").exists()).toBe(false);
        wrapper.unmount();
    });
});
