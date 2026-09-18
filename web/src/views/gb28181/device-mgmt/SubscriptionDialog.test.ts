import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SubscriptionDialog from "./SubscriptionDialog.vue";

const api = vi.hoisted(() => ({
    listDeviceSubscriptions: vi.fn(),
    renewDeviceSubscription: vi.fn(),
    updateDeviceSubscription: vi.fn()
}));

vi.mock("./api", () => api);

const subscriptions = [
    { kind: "catalog", enabled: false, status: "disabled", expiresSeconds: 3600, intervalSeconds: 0, lastError: "" },
    { kind: "mobile_position", enabled: false, status: "disabled", expiresSeconds: 3600, intervalSeconds: 30, lastError: "" },
    { kind: "alarm", enabled: false, status: "disabled", expiresSeconds: 3600, intervalSeconds: 0, lastError: "" },
    { kind: "ptz_precise_position", enabled: false, status: "disabled", expiresSeconds: 3600, intervalSeconds: 0, lastError: "" }
];

function mountDialog() {
    return mount(SubscriptionDialog, {
        props: { visible: true, deviceId: 7, deviceName: "测试设备" },
        global: {
            stubs: {
                "a-modal": { props: ["visible"], template: "<div v-if='visible'><slot name='title' /><slot /></div>" },
                "a-input-number": { template: "<input />" },
                "a-button": { template: "<button @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
                "a-switch": { props: ["modelValue"], template: "<button role='switch' @click='$emit(`change`, true)' />" }
            }
        }
    });
}

describe("SubscriptionDialog", () => {
    beforeEach(() => {
        api.listDeviceSubscriptions.mockReset();
        api.renewDeviceSubscription.mockReset();
        api.updateDeviceSubscription.mockReset();
        api.listDeviceSubscriptions.mockResolvedValue({ code: 0, data: { list: subscriptions } });
        api.updateDeviceSubscription.mockResolvedValue({ code: 0, data: { ...subscriptions[3], enabled: true, status: "active" } });
    });

    it("shows the PTZ precise-position subscription", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        expect(wrapper.text()).toContain("PTZ 精准位置订阅");
    });

    it("updates the PTZ precise-position subscription", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        const ptzRow = wrapper.findAll(".subscription-row").find(row => row.text().includes("PTZ 精准位置订阅"));
        expect(ptzRow).toBeDefined();
        await ptzRow!.get("[role='switch']").trigger("click");
        await flushPromises();

        expect(api.updateDeviceSubscription).toHaveBeenCalledWith(7, "ptz_precise_position", { enabled: true });
    });
});
