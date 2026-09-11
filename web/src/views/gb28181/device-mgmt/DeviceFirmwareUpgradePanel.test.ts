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
                }
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

    it("prefills the vendor and shows field-level validation beside invalid input", async () => {
        const wrapper = mountPanel();
        await flushPromises();

        const inputs = wrapper.findAll("input");
        expect((inputs[1].element as HTMLInputElement).value).toBe("海康");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("ftp://192.0.2.50/firmware.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");

        expect(wrapper.get("[data-testid='file-url-error']").text()).toContain("HTTP 或 HTTPS");
        expect(wrapper.find("[data-testid='firmware-upgrade-confirmation']").exists()).toBe(false);
        wrapper.unmount();
    });

    it("uses separate prepare and confirmation stages, then tracks one submitted task", async () => {
        const onOperationUpdated = vi.fn();
        const wrapper = mountPanel({}, { onOperationUpdated });
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("http://192.0.2.50/firmware/V5.9.0.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");

        expect(wrapper.get("[data-testid='firmware-upgrade-confirmation']").text()).toContain("V5.8.0 → V5.9.0");
        expect(wrapper.find("[data-testid='firmware-upgrade-form']").exists()).toBe(false);
        await wrapper.get("[data-testid='firmware-upgrade-cancel']").trigger("click");
        expect(wrapper.find("[data-testid='firmware-upgrade-form']").exists()).toBe(true);

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
        expect(wrapper.get("[data-testid='firmware-upgrade-tracking']").text()).toContain("已受理");
        expect(wrapper.find("[data-testid='firmware-upgrade-history']").exists()).toBe(false);
        expect(onOperationUpdated).toHaveBeenCalledWith(expect.objectContaining({ operationId: "upgrade-op-1" }));
        wrapper.unmount();
    });

    it("recovers an unfinished operation after close and reopen and emits the operation cache event", async () => {
        const accepted = operation();
        const onOperationUpdated = vi.fn();
        api.listFirmwareUpgrades.mockResolvedValue(response([accepted]));
        const wrapper = mountPanel({}, { onOperationUpdated });
        await flushPromises();
        expect(wrapper.get("[data-testid='firmware-upgrade-tracking']").text()).toContain("已受理");

        await wrapper.setProps({ visible: false });
        await wrapper.setProps({ visible: true });
        await flushPromises();
        expect(wrapper.get("[data-testid='firmware-upgrade-tracking']").text()).toContain("已受理");
        expect(api.upgradeDeviceFirmware).not.toHaveBeenCalled();
        expect(onOperationUpdated).toHaveBeenCalledWith(expect.objectContaining({ operationId: accepted.operationId }));
        wrapper.unmount();
    });

    it("recovers an expired operation as unknown and does not expose a repeat submission", async () => {
        api.listFirmwareUpgrades.mockResolvedValue(response([operation({ deadlineAt: "2020-01-01T00:00:00Z" })]));
        const onBusy = vi.fn();
        const wrapper = mountPanel({}, { onBusy });
        await flushPromises();

        expect(wrapper.text()).toContain("结果未知");
        expect(wrapper.text()).toContain("暂不要重复提交");
        expect(wrapper.find("[data-testid='firmware-upgrade-submit']").exists()).toBe(false);
        expect(onBusy).toHaveBeenLastCalledWith(true);
        expect(api.upgradeDeviceFirmware).not.toHaveBeenCalled();
        wrapper.unmount();
    });

    it("shows SIP and operation identifiers only in technical details and opens records", async () => {
        const onViewRecords = vi.fn();
        const accepted = operation();
        const failed = operation({ status: "failed", errorMessage: "设备升级失败", failedReason: "02" });
        api.listFirmwareUpgrades
            .mockResolvedValueOnce(response([accepted]))
            .mockResolvedValueOnce(response([failed]));
        vi.useFakeTimers();
        const wrapper = mountPanel({}, { onViewRecords });
        await flushPromises();
        await vi.advanceTimersByTimeAsync(2000);
        await flushPromises();

        expect(wrapper.text()).toContain("升级包损坏");
        expect(wrapper.text()).toContain("设备升级失败");
        expect(wrapper.text()).toContain("操作 ID");
        expect(wrapper.text()).toContain("upgrade-session-1");
        expect(wrapper.text()).toContain("200");
        await wrapper.get("[data-testid='firmware-upgrade-view-records']").trigger("click");
        expect(onViewRecords).toHaveBeenCalledWith("upgrade-op-1");
        wrapper.unmount();
    });

    it("marks a network failure uncertain, prevents a second POST, and asks the parent to cache it", async () => {
        api.upgradeDeviceFirmware.mockRejectedValueOnce(new Error("网络不可用"));
        const onSubmissionUncertain = vi.fn();
        const wrapper = mountPanel({}, { onSubmissionUncertain });
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("https://192.0.2.50/firmware/V5.9.0.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        await wrapper.get("[data-testid='firmware-upgrade-confirm']").trigger("click");
        await flushPromises();

        expect(wrapper.text()).toContain("网络不可用");
        expect(wrapper.text()).toContain("暂不要重复提交");
        expect(wrapper.find("[data-testid='firmware-upgrade-submit']").exists()).toBe(false);
        expect(onSubmissionUncertain).toHaveBeenCalledTimes(1);
        expect(api.upgradeDeviceFirmware).toHaveBeenCalledTimes(1);
        await wrapper.setProps({ visible: false });
        await wrapper.setProps({ visible: true });
        await flushPromises();
        expect(api.upgradeDeviceFirmware).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it("emits firmwareUpdated and requires an explicit action before preparing a new upgrade", async () => {
        const succeeded = operation({ status: "succeeded", currentFirmware: "V5.9.0", completedAt: "2026-09-05T03:01:00Z" });
        api.upgradeDeviceFirmware.mockResolvedValueOnce({ code: 0, message: "", data: succeeded });
        const onFirmwareUpdated = vi.fn();
        const wrapper = mountPanel({}, { onFirmwareUpdated });
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("https://192.0.2.50/firmware/V5.9.0.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        await wrapper.get("[data-testid='firmware-upgrade-confirm']").trigger("click");
        await flushPromises();

        expect(onFirmwareUpdated).toHaveBeenCalledWith("V5.9.0");
        expect(wrapper.get("[data-testid='firmware-upgrade-new']").text()).toContain("准备新升级");
        await wrapper.get("[data-testid='firmware-upgrade-new']").trigger("click");
        expect(wrapper.find("[data-testid='firmware-upgrade-tracking']").exists()).toBe(false);
        expect((wrapper.findAll("input")[1].element as HTMLInputElement).value).toBe("海康");
        wrapper.unmount();
    });

    it("keeps real stage labels while polling from accepted to success", async () => {
        vi.useFakeTimers();
        const accepted = operation();
        const succeeded = operation({ status: "succeeded", currentFirmware: "V5.9.0", completedAt: "2026-09-05T03:01:00Z" });
        api.listFirmwareUpgrades
            .mockResolvedValueOnce(response([accepted]))
            .mockResolvedValueOnce(response([succeeded]));
        const onFirmwareUpdated = vi.fn();
        const wrapper = mountPanel({}, { onFirmwareUpdated });
        await flushPromises();
        expect(wrapper.text()).toContain("设备已受理，等待升级完成");

        await vi.advanceTimersByTimeAsync(2000);
        await flushPromises();
        expect(wrapper.text()).toContain("设备已完成升级");
        expect(wrapper.text()).not.toMatch(/\d+%/);
        expect(onFirmwareUpdated).toHaveBeenCalledWith("V5.9.0");
        wrapper.unmount();
    });
    it("shows a new interlock during confirmation and does not submit", async () => {
        const wrapper = mountPanel();
        await flushPromises();
        const inputs = wrapper.findAll("input");
        await inputs[0].setValue("V5.9.0");
        await inputs[2].setValue("http://192.0.2.50/firmware.bin");
        await wrapper.get("[data-testid='firmware-upgrade-submit']").trigger("click");
        await wrapper.setProps({ blockedReason: "设备重启请求处理中" });
        expect(wrapper.get("[data-testid='firmware-upgrade-confirm']").attributes("disabled")).toBeDefined();
        expect(wrapper.get("[data-testid='firmware-upgrade-confirmation']").text()).toContain("设备重启请求处理中");
        await wrapper.get("[data-testid='firmware-upgrade-confirm']").trigger("click");
        expect(api.upgradeDeviceFirmware).not.toHaveBeenCalled();
        wrapper.unmount();
    });

});
