import { flushPromises, mount, type Stubs } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceRebootDialog from "./DeviceRebootDialog.vue";
import type { DeviceOperationResult } from "./api";

const api = vi.hoisted(() => ({
    getDevice: vi.fn(),
    rebootDevice: vi.fn()
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

const secondDevice = {
    ...onlineDevice,
    id: 32,
    deviceId: "34020000001320000002",
    name: "园区 DVR",
    alias: "南门录像机"
};

type Device = typeof onlineDevice;
type DialogOptions = {
    device?: Device;
    canReboot?: boolean;
    blockedReason?: string;
    result?: DeviceOperationResult | null;
    onDeviceUpdated?: (device: Device) => void;
    onOperationUpdated?: (result: DeviceOperationResult) => void;
    onViewRecords?: (operationId?: string) => void;
    onSubmissionUncertain?: () => void;
};

function mountDialog(options: DialogOptions = {}) {
    const stubs: Stubs = {
        "a-modal": {
            props: ["visible", "width", "maskClosable"],
            template: "<div v-if='visible' data-testid='device-reboot-modal' :data-width='width' :data-mask-closable='maskClosable'><slot name='title' /><slot /></div>"
        },
        "a-spin": {
            props: ["loading"],
            template: "<div :data-loading='loading'><slot /></div>"
        },
        "a-button": {
            props: ["disabled", "loading"],
            template: "<button :disabled='disabled' :aria-busy='loading' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
        }
    };
    const attrs: Record<string, unknown> = {};
    if (options.onDeviceUpdated) attrs.onDeviceUpdated = options.onDeviceUpdated;
    if (options.onOperationUpdated) attrs.onOperationUpdated = options.onOperationUpdated;
    if (options.onViewRecords) attrs.onViewRecords = options.onViewRecords;
    if (options.onSubmissionUncertain) attrs.onSubmissionUncertain = options.onSubmissionUncertain;
    return mount(DeviceRebootDialog, {
        props: {
            visible: true,
            device: options.device || onlineDevice,
            canReboot: options.canReboot ?? true,
            blockedReason: options.blockedReason || "",
            result: options.result || null
        },
        attrs,
        global: { stubs }
    });
}

function rebootResult(overrides: Record<string, unknown> = {}) {
    return {
        operationId: "op-2",
        action: "teleboot",
        status: "sent",
        responseRequired: false,
        targetScope: "device",
        targetCode: onlineDevice.deviceId,
        ...overrides
    };
}

describe("DeviceRebootDialog", () => {
    beforeEach(() => {
        api.getDevice.mockReset().mockResolvedValue({ code: 0, message: "", data: onlineDevice });
        api.rebootDevice.mockReset().mockResolvedValue({ code: 0, message: "", data: rebootResult() });
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("opens directly on the one-step confirmation page with a fresh device detail", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        expect(api.getDevice).toHaveBeenCalledWith(31);
        expect(wrapper.text()).toContain("确认重启设备");
        expect(wrapper.text()).toContain("北门录像机");
        expect(wrapper.text()).toContain("34020000001320000001");
        expect(wrapper.text()).toContain("设备在线");
        expect(wrapper.text()).toContain("整台设备及所属通道");
        expect(wrapper.text()).not.toContain("升级记录");
        expect(wrapper.text()).not.toContain("审计记录");
        expect(wrapper.get("[data-testid='device-reboot-modal']").attributes("data-width")).toContain("480px");
        wrapper.unmount();
    });

    it("keeps the submit button disabled while the device detail is loading", async () => {
        let resolveDevice!: (value: unknown) => void;
        api.getDevice.mockReturnValueOnce(new Promise(resolve => { resolveDevice = resolve; }));
        const wrapper = mountDialog();

        const confirm = wrapper.get("[data-testid='device-reboot-confirm']");
        expect(confirm.attributes("disabled")).toBeDefined();
        await confirm.trigger("click");
        expect(api.rebootDevice).not.toHaveBeenCalled();

        resolveDevice({ code: 0, message: "", data: onlineDevice });
        await flushPromises();
        expect(wrapper.get("[data-testid='device-reboot-confirm']").attributes("disabled")).toBeUndefined();
        wrapper.unmount();
    });

    it("cancels without posting", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-cancel']").trigger("click");

        expect(api.rebootDevice).not.toHaveBeenCalled();
        expect(wrapper.emitted("update:visible")?.[0]).toEqual([false]);
        wrapper.unmount();
    });

    it("posts exactly once after confirmation and shows a neutral sent result", async () => {
        const onOperationUpdated = vi.fn();
        const onViewRecords = vi.fn();
        const wrapper = mountDialog({ onOperationUpdated, onViewRecords });
        await flushPromises();

        const confirm = wrapper.get("[data-testid='device-reboot-confirm']");
        await confirm.trigger("click");
        await confirm.trigger("click");
        await flushPromises();

        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        expect(api.rebootDevice).toHaveBeenCalledWith(31, expect.objectContaining({
            confirmed: true,
            idempotencyKey: expect.any(String)
        }));
        expect(onOperationUpdated).toHaveBeenCalledWith(rebootResult());
        expect(wrapper.text()).toContain("请求已发送");
        expect(wrapper.text()).not.toContain("重启成功");
        expect(wrapper.find("[data-testid='device-reboot-confirm']").exists()).toBe(false);

        await wrapper.get("[data-testid='device-reboot-view-records']").trigger("click");
        expect(onViewRecords).toHaveBeenCalledWith("op-2");
        wrapper.unmount();
    });

    it("shows accepted as neutral and handles a deduplicated response", async () => {
        const onOperationUpdated = vi.fn();
        api.rebootDevice.mockResolvedValueOnce({
            code: 0,
            message: "",
            data: rebootResult({ status: "accepted", deduplicated: true })
        });
        const wrapper = mountDialog({ onOperationUpdated });
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();

        expect(onOperationUpdated).toHaveBeenCalledWith(rebootResult({ status: "accepted", deduplicated: true }));
        expect(wrapper.text()).toContain("请求已受理");
        expect(wrapper.text()).toContain("已有相同重启请求");
        expect(wrapper.text()).not.toContain("重启成功");
        wrapper.unmount();
    });

    it("does not post without permission, when offline, or while blocked by another operation", async () => {
        const cases: Array<{ name: string; options: DialogOptions; text: string }> = [
            { name: "permission", options: { canReboot: false }, text: "没有设备重启权限" },
            { name: "offline", options: { device: { ...onlineDevice, online: false, status: 0 } }, text: "设备离线" },
            { name: "blocked", options: { blockedReason: "设备升级处理中，暂不能重启" }, text: "设备升级处理中，暂不能重启" }
        ];

        for (const item of cases) {
            api.getDevice.mockResolvedValueOnce({ code: 0, message: "", data: item.options.device || onlineDevice });
            const wrapper = mountDialog(item.options);
            await flushPromises();
            const confirm = wrapper.get("[data-testid='device-reboot-confirm']");
            expect(wrapper.text(), item.name).toContain(item.text);
            expect(confirm.attributes("disabled"), item.name).toBeDefined();
            await confirm.trigger("click");
            expect(api.rebootDevice, item.name).not.toHaveBeenCalled();
            wrapper.unmount();
        }
    });

    it("allows reboot when the whole device is online even if every channel is offline", async () => {
        const device = { ...onlineDevice, channelOnlineCount: 0, onlineRate: 0 };
        api.getDevice.mockResolvedValueOnce({ code: 0, message: "", data: device });
        const wrapper = mountDialog({ device });
        await flushPromises();

        expect(wrapper.get("[data-testid='device-reboot-confirm']").attributes("disabled")).toBeUndefined();
        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it("does not post after the device detail GET fails", async () => {
        api.getDevice.mockRejectedValueOnce(new Error("设备详情服务不可用"));
        const wrapper = mountDialog();
        await flushPromises();

        expect(wrapper.text()).toContain("设备信息加载失败");
        const confirm = wrapper.get("[data-testid='device-reboot-confirm']");
        expect(confirm.attributes("disabled")).toBeDefined();
        await confirm.trigger("click");
        expect(api.rebootDevice).not.toHaveBeenCalled();
        wrapper.unmount();
    });

    it("shows an unknown result after a transport error and disallows direct retry", async () => {
        const onSubmissionUncertain = vi.fn();
        const onViewRecords = vi.fn();
        api.rebootDevice.mockRejectedValueOnce(new Error("Network Error"));
        const wrapper = mountDialog({ onSubmissionUncertain, onViewRecords });
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();

        expect(onSubmissionUncertain).toHaveBeenCalledTimes(1);
        expect(wrapper.text()).toContain("结果未知");
        expect(wrapper.text()).toContain("请在维护记录中确认");
        expect(wrapper.find("[data-testid='device-reboot-confirm']").exists()).toBe(false);
        await wrapper.get("[data-testid='device-reboot-view-records']").trigger("click");
        expect(onViewRecords).toHaveBeenCalledWith(undefined);
        wrapper.unmount();
    });

    it("ignores a stale response when switching to another device", async () => {
        let resolveReboot!: (value: unknown) => void;
        api.rebootDevice.mockReturnValueOnce(new Promise(resolve => { resolveReboot = resolve; }));
        api.getDevice
            .mockResolvedValueOnce({ code: 0, message: "", data: onlineDevice })
            .mockResolvedValueOnce({ code: 0, message: "", data: secondDevice });
        const onOperationUpdated = vi.fn();
        const wrapper = mountDialog({ onOperationUpdated });
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await wrapper.setProps({ device: secondDevice });
        await flushPromises();
        resolveReboot({ code: 0, message: "", data: rebootResult({ operationId: "stale-op" }) });
        await flushPromises();

        expect(wrapper.text()).toContain("南门录像机");
        expect(wrapper.text()).not.toContain("stale-op");
        expect(onOperationUpdated).not.toHaveBeenCalled();
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it("does not reload when the parent echoes an updated DTO for the same device", async () => {
        let wrapper!: ReturnType<typeof mountDialog>;
        const onDeviceUpdated = vi.fn((updated: Device) => {
            void wrapper.setProps({ device: { ...updated, alias: "父级回声" } });
        });
        wrapper = mountDialog({ onDeviceUpdated });
        await flushPromises();
        await flushPromises();

        expect(onDeviceUpdated).toHaveBeenCalled();
        expect(api.getDevice).toHaveBeenCalledTimes(1);
        expect(wrapper.text()).toContain("父级回声");
        wrapper.unmount();
    });

    it("renders and updates a parent-provided operation result without another POST", async () => {
        const wrapper = mountDialog({ result: rebootResult({ status: "pending" }) });
        await flushPromises();

        expect(wrapper.text()).toContain("请求处理中");
        expect(wrapper.find("[data-testid='device-reboot-confirm']").exists()).toBe(false);
        await wrapper.setProps({ result: rebootResult({ status: "accepted" }) });
        await flushPromises();
        expect(wrapper.text()).toContain("请求已受理");
        expect(api.rebootDevice).not.toHaveBeenCalled();
        wrapper.unmount();
    });

    it("allows a new confirmation after closing a terminal sent result, and cancel still does not post", async () => {
        const wrapper = mountDialog();
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();
        expect(wrapper.text()).toContain("请求已发送");

        await wrapper.setProps({ visible: false, result: null });
        await wrapper.setProps({ visible: true, result: null });
        await flushPromises();

        expect(wrapper.find("[data-testid='device-reboot-confirm']").exists()).toBe(true);
        await wrapper.get("[data-testid='device-reboot-cancel']").trigger("click");
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it.each([
        ["queued", { status: "queued" }],
        ["pending", { status: "pending" }],
        ["accepted", { status: "accepted" }],
        ["sent while awaiting a response", { status: "sent", responseRequired: true }]
    ])("keeps a %s local operation from being submitted again after reopening", async (_name, overrides) => {
        api.rebootDevice.mockResolvedValueOnce({ code: 0, message: "", data: rebootResult(overrides) });
        const wrapper = mountDialog();
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();
        await wrapper.setProps({ visible: false, result: null });
        await wrapper.setProps({ visible: true, result: null });
        await flushPromises();

        expect(wrapper.find("[data-testid='device-reboot-confirm']").exists()).toBe(false);
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);
        wrapper.unmount();
    });

    it("keeps the modal and cancel action locked while POST is pending", async () => {
        let resolveReboot!: (value: unknown) => void;
        api.rebootDevice.mockReturnValueOnce(new Promise(resolve => { resolveReboot = resolve; }));
        const wrapper = mountDialog();
        await flushPromises();

        await wrapper.get("[data-testid='device-reboot-confirm']").trigger("click");
        await flushPromises();

        const modal = wrapper.get("[data-testid='device-reboot-modal']");
        expect(modal.attributes("data-mask-closable")).toBe("false");
        expect(wrapper.get("[data-testid='device-reboot-cancel']").attributes("disabled")).toBeDefined();
        expect(api.rebootDevice).toHaveBeenCalledTimes(1);

        resolveReboot({ code: 0, message: "", data: rebootResult() });
        await flushPromises();
        wrapper.unmount();
    });
});
