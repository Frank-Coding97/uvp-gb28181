import { flushPromises, mount, type Stubs } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceMaintenanceDialog from "./DeviceMaintenanceDialog.vue";

const api = vi.hoisted(() => ({
    getDevice: vi.fn(),
    listMaintenanceOperations: vi.fn(),
    rebootDevice: vi.fn(),
    listFirmwareUpgrades: vi.fn(),
    upgradeDeviceFirmware: vi.fn()
}));

vi.mock("./api", () => api);

const onlineDevice = {
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

function mountDialog(device = onlineDevice, canReboot = true, onDeviceUpdated?: (updated: typeof onlineDevice) => void, withUpgradePanel = false) {
    const stubs: Stubs = {
        "a-modal": {
            props: ["visible"],
            template: "<div v-if='visible' data-testid='maintenance-modal'><slot name='title' /><slot /></div>"
        },
        "a-spin": { template: "<div><slot /></div>" },
        "a-input": {
            props: ["modelValue", "disabled"],
            template: "<input :value='modelValue' :disabled='disabled' @input='$emit(`update:modelValue`, $event.target.value)' />"
        },
        "a-button": {
            props: ["disabled", "loading"],
            template: "<button :disabled='disabled' :aria-busy='loading' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
        },
        "a-pagination": {
            props: ["current", "pageSize", "total"],
            template: "<button data-testid='maintenance-next-page' @click='$emit(`change`, current + 1)'>下一页</button>"
        },
        "a-empty": { template: "<div>暂无维护记录</div>" }
    };
    if (!withUpgradePanel) stubs.DeviceFirmwareUpgradePanel = { template: "<div data-testid='firmware-upgrade-panel' />" };
    return mount(DeviceMaintenanceDialog, {
        props: { visible: true, device, canReboot, canUpgrade: true },
        attrs: onDeviceUpdated ? { onDeviceUpdated } : undefined,
        global: { stubs }
    });
}

function operation(overrides: Record<string, unknown> = {}) {
    return {
        operationId: "op-1",
        action: "teleboot",
        status: "sent",
        sipStatus: 200,
        errorMessage: "",
        actorId: 42,
        createdAt: "2026-09-05T03:00:00Z",
        sentAt: "2026-09-05T03:00:01Z",
        completedAt: null,
        responseRequired: false,
        targetCode: onlineDevice.deviceId,
        ...overrides
    };
}

function firmwareOperation(overrides: Record<string, unknown> = {}) {
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

describe("DeviceMaintenanceDialog", () => {
    beforeEach(() => {
        api.getDevice.mockReset().mockResolvedValue({ code: 0, message: "", data: onlineDevice });
        api.listMaintenanceOperations.mockReset().mockResolvedValue({
            code: 0,
            message: "",
            data: { list: [operation()], total: 1, page: 1, pageSize: 10 }
        });
        api.rebootDevice.mockReset().mockResolvedValue({
            code: 0,
            message: "",
            data: { operationId: "op-2", action: "teleboot", status: "sent", responseRequired: false, targetCode: onlineDevice.deviceId }
        });
        api.listFirmwareUpgrades.mockReset().mockResolvedValue({
            code: 0,
            message: "",
            data: { list: [], total: 0, page: 1, pageSize: 10 }
        });
        api.upgradeDeviceFirmware.mockReset().mockResolvedValue({ code: 0, message: "", data: null });
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("loads the actual device detail and paged maintenance records", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        expect(api.getDevice).toHaveBeenCalledWith(31);
        expect(api.listMaintenanceOperations).toHaveBeenCalledWith(31, { page: 1, pageSize: 10 });
        expect(wrapper.text()).toContain("北门录像机");
        expect(wrapper.text()).toContain("34020000001320000001");
        expect(wrapper.text()).toContain("海康 / DS-7608");
        expect(wrapper.text()).toContain("V5.8.0");
        expect(wrapper.text()).toContain("7 / 8");
        expect(wrapper.text()).toContain("userId 42");
        expect(wrapper.text()).toContain("已发送");
        wrapper.unmount();
    });

    it("does not restart loading when the parent replaces the same-device DTO", async () => {
        const wrapper = mountDialog();
        await flushPromises();
        const getDeviceCalls = api.getDevice.mock.calls.length;

        await wrapper.setProps({ device: { ...onlineDevice, online: false, status: 0 } });
        await flushPromises();

        expect(api.getDevice).toHaveBeenCalledTimes(getDeviceCalls);
        expect(wrapper.text()).toContain("离线");
        expect(wrapper.get("[data-testid='maintenance-reboot']").attributes("disabled")).toBeDefined();
        wrapper.unmount();
    });

    it("keeps an emit-driven parent DTO echo finite for the same device", async () => {
        let wrapper!: ReturnType<typeof mountDialog>;
        const onDeviceUpdated = vi.fn((updated: typeof onlineDevice) => {
            void wrapper.setProps({ device: { ...onlineDevice, ...updated } });
        });
        wrapper = mountDialog(onlineDevice, true, onDeviceUpdated);
        await flushPromises();
        await flushPromises();

        expect(onDeviceUpdated).toHaveBeenCalled();
        expect(api.getDevice).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it("keeps reboot unavailable for an offline device", async () => {
        const offline = { ...onlineDevice, online: false, status: 0 };
        api.getDevice.mockResolvedValueOnce({ code: 0, message: "", data: offline });
        const wrapper = mountDialog(offline);
        await flushPromises();

        const reboot = wrapper.get("[data-testid='maintenance-reboot']");
        expect(reboot.attributes("disabled")).toBeDefined();
        await reboot.trigger("click");
        expect(api.rebootDevice).not.toHaveBeenCalled();
        expect(wrapper.text()).toContain("设备离线");
        wrapper.unmount();
    });

    it("requires confirmation, does not post on cancel, and prevents duplicate pending clicks", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        await wrapper.get("[data-testid='maintenance-reboot']").trigger("click");
        expect(wrapper.text()).toContain("确认重启整台设备");
        await wrapper.get("[data-testid='maintenance-reboot-cancel']").trigger("click");
        expect(api.rebootDevice).not.toHaveBeenCalled();

        await wrapper.get("[data-testid='maintenance-reboot']").trigger("click");
        let resolveReboot!: (value: unknown) => void;
        api.rebootDevice.mockReturnValueOnce(new Promise(resolve => { resolveReboot = resolve; }));
        await wrapper.get("[data-testid='maintenance-reboot-confirm']").trigger("click");
        await flushPromises();
        expect(wrapper.get("[data-testid='maintenance-reboot']").attributes("disabled")).toBeDefined();
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        expect(api.rebootDevice).toHaveBeenCalledWith(31, expect.objectContaining({ confirmed: true, idempotencyKey: expect.any(String) }));
        resolveReboot({
            code: 0,
            message: "",
            data: { operationId: "op-2", action: "teleboot", status: "sent", responseRequired: false, targetCode: onlineDevice.deviceId }
        });
        await flushPromises();
        expect(api.getDevice).toHaveBeenCalledWith(31);
        wrapper.unmount();
    });

    it("shows unknown when the operation has no usable completion result", async () => {
        api.listMaintenanceOperations.mockResolvedValueOnce({
            code: 0,
            message: "",
            data: { list: [operation({ status: "timeout", errorMessage: "查询超时", actorId: 0 })], total: 1, page: 1, pageSize: 10 }
        });
        const wrapper = mountDialog();
        await flushPromises();

        expect(wrapper.text()).toContain("结果未知");
        expect(wrapper.text()).toContain("未记录");
        expect(wrapper.text()).toContain("查询超时");
        wrapper.unmount();
    });

    it("renders a rejected operation as a terminal failure", async () => {
        api.listMaintenanceOperations.mockResolvedValueOnce({
            code: 0,
            message: "",
            data: { list: [operation({ status: "rejected", sipStatus: 403, errorMessage: "设备拒绝执行" })], total: 1, page: 1, pageSize: 10 }
        });
        const wrapper = mountDialog();
        await flushPromises();

        expect(wrapper.text()).toContain("已拒绝");
        expect(wrapper.text()).toContain("设备拒绝执行");
        wrapper.unmount();
    });

    it("stops polling after sixty seconds when the operation has no deadline", async () => {
        vi.useFakeTimers();
        const pendingOperation = operation({ status: "queued", responseRequired: true, sipStatus: 0 });
        api.listMaintenanceOperations.mockResolvedValue({
            code: 0,
            message: "",
            data: { list: [pendingOperation], total: 1, page: 1, pageSize: 10 }
        });
        api.rebootDevice.mockResolvedValueOnce({
            code: 0,
            message: "",
            data: { operationId: "op-queue", action: "teleboot", status: "queued", responseRequired: true, targetCode: onlineDevice.deviceId }
        });

        const wrapper = mountDialog();
        await flushPromises();
        await wrapper.get("[data-testid='maintenance-reboot']").trigger("click");
        await wrapper.get("[data-testid='maintenance-reboot-confirm']").trigger("click");
        await flushPromises();

        await vi.advanceTimersByTimeAsync(61_000);
        await flushPromises();
        const callsAfterDeadline = api.listMaintenanceOperations.mock.calls.length;
        expect(wrapper.text()).toContain("结果未知");
        expect(wrapper.text()).toContain("无响应");

        await vi.advanceTimersByTimeAsync(10_000);
        expect(api.listMaintenanceOperations).toHaveBeenCalledTimes(callsAfterDeadline);
        wrapper.unmount();
    });

    it("keeps reboot copy neutral while an accepted firmware upgrade disables reboot, then unlocks after success", async () => {
        vi.useFakeTimers();
        const accepted = firmwareOperation();
        const succeeded = firmwareOperation({ status: "succeeded", currentFirmware: "V5.9.0", completedAt: "2026-09-05T03:01:00Z" });
        api.listFirmwareUpgrades
            .mockResolvedValueOnce({ code: 0, message: "", data: { list: [accepted], total: 1, page: 1, pageSize: 10 } })
            .mockResolvedValueOnce({ code: 0, message: "", data: { list: [succeeded], total: 1, page: 1, pageSize: 10 } });
        const wrapper = mountDialog(onlineDevice, true, undefined, true);
        await flushPromises();

        const reboot = wrapper.get("[data-testid='maintenance-reboot']");
        expect(reboot.attributes("disabled")).toBeDefined();
        expect(reboot.text()).toContain("重启设备");
        expect(reboot.text()).not.toContain("请求处理中");
        expect(wrapper.text()).toContain("升级任务尚未确认完成，暂不能重启设备");

        await vi.advanceTimersByTimeAsync(2000);
        await flushPromises();
        expect(wrapper.text()).toContain("升级成功");
        expect(wrapper.get("[data-testid='maintenance-reboot']").attributes("disabled")).toBeUndefined();
        expect(wrapper.text()).toContain("V5.9.0");
        wrapper.unmount();
    });

    it("keeps reboot disabled for an unknown firmware result without calling it reboot processing", async () => {
        api.listFirmwareUpgrades.mockResolvedValueOnce({
            code: 0,
            message: "",
            data: { list: [firmwareOperation({ status: "unknown", errorMessage: "等待设备升级结果超时" })], total: 1, page: 1, pageSize: 10 }
        });
        const wrapper = mountDialog(onlineDevice, true, undefined, true);
        await flushPromises();

        const reboot = wrapper.get("[data-testid='maintenance-reboot']");
        expect(reboot.attributes("disabled")).toBeDefined();
        expect(reboot.text()).toContain("重启设备");
        expect(reboot.text()).not.toContain("请求处理中");
        expect(wrapper.text()).toContain("上一次升级请求结果未知");
        expect(wrapper.text()).toContain("结果未知");
        wrapper.unmount();
    });
});
