import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DeviceVO, MaintenanceOperation, UpgradeOperation } from "./api";
import DeviceMaintenanceRecordsDrawer from "./DeviceMaintenanceRecordsDrawer.vue";

const api = vi.hoisted(() => ({
    listMaintenanceOperations: vi.fn(),
    listFirmwareUpgrades: vi.fn()
}));

vi.mock("./api", async importOriginal => ({
    ...await importOriginal<typeof import("./api")>(),
    ...api
}));

const device = (id = 31): DeviceVO => ({
    id,
    deviceId: `340200000013200000${id}`,
    name: id === 31 ? "园区 NVR" : "西区 NVR",
    alias: id === 31 ? "北门录像机" : "西门录像机",
    transport: "TCP",
    manufacturer: "海康",
    model: "DS-7608",
    firmware: "V5.8.0",
    ip: "192.0.2.31",
    port: 5060,
    status: id === 31 ? 1 : 0,
    online: id === 31,
    channelCount: 8,
    channelOnlineCount: 7,
    onlineRate: 87.5,
    createdAt: "2026-09-05T01:00:00Z",
    updatedAt: "2026-09-05T02:00:00Z",
    ownerDeptId: 1
});

const rebootOperation = (overrides: Partial<MaintenanceOperation> = {}): MaintenanceOperation => ({
    operationId: "reboot-op-1",
    action: "teleboot",
    status: "sent",
    sipStatus: 200,
    errorMessage: "",
    actorId: 42,
    createdAt: "2026-09-05T03:00:00Z",
    sentAt: "2026-09-05T03:00:01Z",
    completedAt: null,
    responseRequired: false,
    targetCode: device().deviceId,
    ...overrides
});

const upgradeOperation = (overrides: Partial<UpgradeOperation> = {}): UpgradeOperation => ({
    operationId: "upgrade-op-1",
    sessionId: "upgrade-session-1",
    deviceId: 31,
    status: "succeeded",
    firmware: "V5.9.0",
    currentFirmware: "V5.9.0",
    manufacturer: "海康",
    sipStatus: 200,
    errorCode: "",
    errorMessage: "",
    failedReason: "",
    actorId: 7,
    createdAt: "2026-09-05T04:00:00Z",
    sentAt: "2026-09-05T04:00:01Z",
    acceptedAt: "2026-09-05T04:00:02Z",
    completedAt: "2026-09-05T04:02:00Z",
    deadlineAt: "2099-09-05T04:02:00Z",
    ...overrides
});

function page<T>(list: T[], total = list.length, current = 1) {
    return { code: 0, message: "", data: { list, total, page: current, pageSize: 10 } };
}

function mountDrawer(props: Record<string, unknown> = {}) {
    return mount(DeviceMaintenanceRecordsDrawer, {
        props: { visible: true, device: device(), ...props },
        global: {
            stubs: {
                "a-drawer": {
                    props: ["visible", "width"],
                    template: "<section v-if='visible' data-testid='maintenance-records-drawer'><slot name='title' /><slot /></section>"
                },
                "a-button": {
                    props: ["disabled", "loading"],
                    template: "<button :disabled='disabled' :aria-busy='loading' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
                },
                "a-pagination": {
                    props: ["current", "pageSize", "total"],
                    template: "<button data-testid='maintenance-records-next-page' @click='$emit(`change`, current + 1)'>下一页</button>"
                }
            }
        }
    });
}

async function settle<T extends VueWrapper<any>>(wrapper: T) {
    await flushPromises();
    await wrapper.vm.$nextTick();
}

describe("DeviceMaintenanceRecordsDrawer", () => {
    beforeEach(() => {
        api.listMaintenanceOperations.mockReset().mockResolvedValue(page([rebootOperation()], 1));
        api.listFirmwareUpgrades.mockReset().mockResolvedValue(page([], 0));
    });

    it("loads only the reboot page first and renders a read-only summary", async () => {
        const wrapper = mountDrawer();
        await settle(wrapper);

        expect(api.listMaintenanceOperations).toHaveBeenCalledWith(31, { page: 1, pageSize: 10 });
        expect(api.listFirmwareUpgrades).not.toHaveBeenCalled();
        expect(wrapper.text()).toContain("重启设备");
        expect(wrapper.text()).toContain("账号 #42");
        expect(wrapper.text()).toContain("已发送");
        expect(wrapper.text()).toContain("平台已发送请求，设备执行结果未回传");
        expect(wrapper.get("[data-testid='maintenance-records-tab-upgrade']").text()).toContain("—");
        expect(wrapper.find("[data-testid='maintenance-records-reboot-action']").exists()).toBe(false);
    });

    it("keeps upgrade paging independent from reboot paging", async () => {
        api.listMaintenanceOperations.mockResolvedValue(page([rebootOperation()], 21));
        api.listFirmwareUpgrades.mockResolvedValue(page([upgradeOperation()], 21));
        const wrapper = mountDrawer();
        await settle(wrapper);

        await wrapper.get("[data-testid='maintenance-records-tab-upgrade']").trigger("click");
        await settle(wrapper);
        expect(api.listFirmwareUpgrades).toHaveBeenCalledWith(31, { page: 1, pageSize: 10 });

        await wrapper.get("[data-testid='maintenance-records-next-page']").trigger("click");
        await settle(wrapper);
        expect(api.listFirmwareUpgrades).toHaveBeenCalledWith(31, { page: 2, pageSize: 10 });
        expect(api.listMaintenanceOperations).not.toHaveBeenCalledWith(31, { page: 2, pageSize: 10 });
    });

    it("opens first-page operation deep links and reports an accurate miss", async () => {
        api.listFirmwareUpgrades.mockResolvedValue(page([upgradeOperation()], 18));
        const wrapper = mountDrawer({ initialType: "upgrade", operationId: "upgrade-op-1" });
        await settle(wrapper);
        expect(wrapper.find("[data-testid='maintenance-record-detail']").exists()).toBe(true);
        expect(wrapper.text()).toContain("upgrade-op-1");

        const missWrapper = mountDrawer({ initialType: "upgrade", operationId: "missing-op" });
        await settle(missWrapper);
        expect(missWrapper.text()).toContain("未在当前页找到操作");
        expect(missWrapper.find("[data-testid='maintenance-record-detail']").exists()).toBe(false);
    });

    it("shows real timestamps, reported firmware, failure reason, and collapsed technical data", async () => {
        api.listFirmwareUpgrades.mockResolvedValue(page([upgradeOperation({
            status: "failed",
            currentFirmware: "V5.8.0",
            failedReason: "02",
            errorMessage: "设备拒绝升级"
        })]));
        const wrapper = mountDrawer({ initialType: "upgrade" });
        await settle(wrapper);
        await wrapper.get("[data-testid='maintenance-record-upgrade-row']").trigger("click");

        expect(wrapper.text()).toContain("创建时间");
        expect(wrapper.text()).toContain("2026-09-05 12:00:00");
        expect(wrapper.text()).toContain("设备回报版本");
        expect(wrapper.text()).toContain("V5.8.0");
        expect(wrapper.text()).toContain("升级包损坏");
        const technical = wrapper.get("[data-testid='maintenance-record-technical']");
        expect(technical.attributes("open")).toBeUndefined();
        await technical.get("summary").trigger("click");
        expect(technical.text()).toContain("upgrade-session-1");
        expect(technical.text()).toContain("SIP 状态200");
    });

    it("ignores stale responses after switching devices", async () => {
        let resolveOld!: (value: unknown) => void;
        api.listMaintenanceOperations.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve; }));
        api.listMaintenanceOperations.mockResolvedValueOnce(page([rebootOperation({ operationId: "new-op", actorId: 84 })]));
        const wrapper = mountDrawer();
        await wrapper.setProps({ device: device(32) });
        await settle(wrapper);
        resolveOld(page([rebootOperation({ operationId: "old-op" })]));
        await settle(wrapper);

        expect(wrapper.text()).toContain("西门录像机");
        expect(wrapper.text()).toContain("账号 #84");
        expect(wrapper.text()).not.toContain("账号 #42");
    });

    it("does not open a deep-linked detail when its tab is no longer active", async () => {
        let resolveUpgrade!: (value: unknown) => void;
        api.listFirmwareUpgrades.mockImplementationOnce(() => new Promise(resolve => { resolveUpgrade = resolve; }));
        const wrapper = mountDrawer({ initialType: "upgrade", operationId: "upgrade-op-1" });
        await wrapper.get("[data-testid='maintenance-records-tab-reboot']").trigger("click");
        resolveUpgrade(page([upgradeOperation()]));
        await settle(wrapper);

        expect(wrapper.find("[data-testid='maintenance-record-detail']").exists()).toBe(false);
        expect(wrapper.text()).toContain("重启记录");
    });

    it("shows a retry state and recovers through GET only", async () => {
        api.listMaintenanceOperations.mockRejectedValueOnce(new Error("网络暂不可用"));
        const wrapper = mountDrawer();
        await settle(wrapper);
        expect(wrapper.get("[role='alert']").text()).toContain("网络暂不可用");

        api.listMaintenanceOperations.mockResolvedValueOnce(page([rebootOperation({ operationId: "retry-op", status: "accepted" })]));
        await wrapper.get("[data-testid='maintenance-records-retry']").trigger("click");
        await settle(wrapper);
        expect(wrapper.text()).toContain("设备已受理，执行结果待回报");
        expect(api.listMaintenanceOperations).toHaveBeenCalledTimes(2);
    });
    it("keeps the selected record in place when refreshing its result", async () => {
        api.listFirmwareUpgrades.mockResolvedValue(page([upgradeOperation({ status: "accepted" })]));
        const wrapper = mountDrawer({ initialType: "upgrade" });
        await settle(wrapper);
        await wrapper.get("[data-testid='maintenance-record-upgrade-row']").trigger("click");
        api.listFirmwareUpgrades.mockResolvedValue(page([upgradeOperation({ status: "succeeded", currentFirmware: "V5.9.0" })]));
        await wrapper.get("[data-testid='maintenance-records-refresh']").trigger("click");
        await settle(wrapper);
        expect(wrapper.find("[data-testid='maintenance-record-detail']").exists()).toBe(true);
        expect(wrapper.get("[data-testid='maintenance-record-detail']").text()).toContain("升级成功");
    });

});
