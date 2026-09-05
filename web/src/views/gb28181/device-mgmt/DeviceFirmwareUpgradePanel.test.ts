import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceFirmwareUpgradePanel from "./DeviceFirmwareUpgradePanel.vue";

const api = vi.hoisted(() => ({
    listFirmwareUpgrades: vi.fn(),
    upgradeDeviceFirmware: vi.fn()
}));

vi.mock("./api", () => api);

const device = {
    id: 31,
    deviceId: "34020000001320000001",
    name: "园区 NVR",
    alias: "北门录像机",
    manufacturer: "海康",
    model: "DS-7608",
    firmware: "V5.8.0",
    effectiveVersion: "2022",
    transport: "TCP",
    ip: "192.0.2.31",
    port: 5060,
    status: 1,
    online: true,
    channelCount: 8,
    channelOnlineCount: 7,
    onlineRate: 87.5,
    createdAt: "2026-09-05T01:00:00Z",
    updatedAt: "2026-09-05T02:00:00Z",
    ownerDeptId: 1
};

function response(list: unknown[] = []) {
    return { code: 0, message: "", data: { list, total: list.length, page: 1, pageSize: 10 } };
}

function operation(overrides: Record<string, unknown> = {}) {
    return {
        operationId: "upgrade-op-1",
        sessionId: "upgrade-session-1",
        deviceId: 31,
        status: "accepted",
        firmware: "V5.9.0",
        currentFirmware: "V5.8.0",
        manufacturer: "海康",
        sipStatus: 200,
        errorCode: "",
        errorMessage: "",
        failedReason: "",
        actorId: 42,
        createdAt: "2026-09-05T03:00:00Z",
        sentAt: "2026-09-05T03:00:01Z",
        acceptedAt: "2026-09-05T03:00:02Z",
        completedAt: null,
        deadlineAt: "2099-09-05T03:00:00Z",
        ...overrides
    };
}

function mountPanel(props: Record<string, unknown> = {}, attrs: Record<string, unknown> = {}) {
    return mount(DeviceFirmwareUpgradePanel, {
        props: { visible: true, device, canUpgrade: true, rebootBusy: false, ...props },
        attrs,
        global: {
            stubs: {
                "a-input": {
                    props: ["modelValue", "disabled"],
                    template: "<input :value='modelValue' :disabled='disabled' @input='$emit(`update:modelValue`, $event.target.value)' />"
                },
                "a-button": {
                    props: ["disabled", "loading"],
                    template: "<button :disabled='disabled' :aria-busy='loading' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
                },
                "a-pagination": { template: "<button />" }
            }
        }
    });
}

describe("DeviceFirmwareUpgradePanel", () => {
    beforeEach(() => {
        api.listFirmwareUpgrades.mockReset().mockResolvedValue(response());
        api.upgradeDeviceFirmware.mockReset().mockResolvedValue({ code: 0, message: "", data: operation() });
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("allows history but disables submission when the device is offline or is not on profile 2022", async () => {
        const offline = mountPanel({ device: { ...device, online: false } });
        await flushPromises();
        expect(offline.text()).toContain("设备离线");
        expect(offline.get("[data-testid='firmware-upgrade-submit']").attributes("disabled")).toBeDefined();
        offline.unmount();

        const legacy = mountPanel({ device: { ...device, effectiveVersion: "2016" } });
        await flushPromises();
        expect(legacy.text()).toContain("GB/T 28181-2022");
        expect(legacy.get("[data-testid='firmware-upgrade-submit']").attributes("disabled")).toBeDefined();
        legacy.unmount();
    });

    it("requires a second confirmation and posts one request with the entered upgrade target", async () => {
        const wrapper = mountPanel();
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("http://192.0.2.50/firmware/V5.9.0.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        expect(wrapper.text()).toContain("确认向设备发起升级");
        await wrapper.get("[data-testid='firmware-upgrade-cancel']").trigger("click");
        expect(api.upgradeDeviceFirmware).not.toHaveBeenCalled();

        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        await wrapper.get("[data-testid='firmware-upgrade-confirm']").trigger("click");
        await flushPromises();
        expect(api.upgradeDeviceFirmware).toHaveBeenCalledTimes(1);
        expect(api.upgradeDeviceFirmware).toHaveBeenCalledWith(31, expect.objectContaining({
            confirmed: true,
            firmware: "V5.9.0",
            fileUrl: "http://192.0.2.50/firmware/V5.9.0.bin",
            manufacturer: "海康",
            idempotencyKey: expect.any(String)
        }));
        wrapper.unmount();
    });

    it("recovers an active operation on open, emits busy, and updates the reported version after success", async () => {
        vi.useFakeTimers();
        const accepted = operation();
        const succeeded = operation({ status: "succeeded", currentFirmware: "V5.9.0", completedAt: "2026-09-05T03:01:00Z" });
        api.listFirmwareUpgrades
            .mockResolvedValueOnce(response([accepted]))
            .mockResolvedValueOnce(response([succeeded]));
        const onBusy = vi.fn();
        const onFirmwareUpdated = vi.fn();
        const wrapper = mountPanel({}, { onBusy, onFirmwareUpdated });
        await flushPromises();
        expect(onBusy).toHaveBeenCalledWith(true);
        expect(wrapper.text()).toContain("已受理");

        await vi.advanceTimersByTimeAsync(2000);
        await flushPromises();
        expect(wrapper.text()).toContain("升级成功");
        expect(onBusy).toHaveBeenLastCalledWith(false);
        expect(onFirmwareUpdated).toHaveBeenCalledWith("V5.9.0");
        wrapper.unmount();
    });

    it("marks an expired active operation as unknown and does not offer a repeat submission", async () => {
        const onBusy = vi.fn();
        const wrapper = mountPanel({}, { onBusy });
        api.listFirmwareUpgrades.mockReset().mockResolvedValue(response([operation({ deadlineAt: "2020-01-01T00:00:00Z" })]));
        await wrapper.setProps({ visible: false });
        await wrapper.setProps({ visible: true });
        await flushPromises();

        expect(wrapper.text()).toContain("结果未知");
        expect(wrapper.text()).toContain("暂不要重复提交");
        expect(wrapper.get("[data-testid='firmware-upgrade-submit']").attributes("disabled")).toBeDefined();
        expect(onBusy).toHaveBeenLastCalledWith(true);
        expect(api.upgradeDeviceFirmware).not.toHaveBeenCalled();
        wrapper.unmount();
    });

    it("shows the standard failure reason alongside the backend summary", async () => {
        api.listFirmwareUpgrades.mockResolvedValueOnce(response([operation({
            status: "failed",
            errorMessage: "设备升级失败",
            failedReason: "02"
        })]));
        const wrapper = mountPanel();
        await flushPromises();

        expect(wrapper.text()).toContain("升级包损坏");
        expect(wrapper.text()).toContain("设备升级失败");
        wrapper.unmount();
    });

    it("keeps a network failure as an unknown result and never retries the POST", async () => {
        api.upgradeDeviceFirmware.mockRejectedValueOnce(new Error("网络不可用"));
        const wrapper = mountPanel();
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("https://192.0.2.50/firmware/V5.9.0.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        await wrapper.get("[data-testid='firmware-upgrade-confirm']").trigger("click");
        await flushPromises();

        expect(wrapper.text()).toContain("网络不可用");
        expect(wrapper.text()).toContain("暂不要重复提交");
        expect(wrapper.get("[data-testid='firmware-upgrade-submit']").attributes("disabled")).toBeDefined();
        expect(api.upgradeDeviceFirmware).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });
});
