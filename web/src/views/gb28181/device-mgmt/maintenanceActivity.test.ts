import { describe, expect, it, vi } from "vitest";
import { createMaintenanceActivity } from "./maintenanceActivity";
import type { DeviceOperationResult, UpgradeOperation } from "./api";

function fixture() {
    const api = {
        listMaintenanceOperations: vi.fn().mockResolvedValue({ code: 0, data: { list: [] } }),
        listFirmwareUpgrades: vi.fn().mockResolvedValue({ code: 0, data: { list: [] } })
    };
    return { api, activity: createMaintenanceActivity(api as any) };
}
const upgrade = (status: string) => ({ operationId: "u1", deviceId: 31, status, firmware: "v2" } as UpgradeOperation);

describe("maintenance activity across operation surfaces", () => {
    it("keeps upgrade blocking reboot after the surface closes and isolates devices", async () => {
        const { activity, api } = fixture();
        activity.rememberUpgrade(31, upgrade("accepted"));
        expect(activity.reason(31, "reboot")).toContain("升级");
        expect(activity.reason(32, "reboot")).toBe("");
        api.listFirmwareUpgrades.mockResolvedValue({ code: 0, data: { list: [upgrade("succeeded")] } });
        await activity.refresh(31);
        expect(activity.reason(31, "reboot")).toBe("");
    });
    it("does not lose a known in-flight operation if it is absent from the fetched page", async () => {
        const { activity } = fixture();
        activity.rememberUpgrade(31, upgrade("accepted"));
        await activity.refresh(31);
        expect(activity.reason(31, "reboot")).toContain("升级");
    });
    it("does not unlock an uncertain submission on closing or an empty GET", async () => {
        const { activity } = fixture();
        activity.markUncertain(31, "upgrade");
        await activity.refresh(31);
        expect(activity.reason(31, "upgrade")).toContain("结果未知");
        expect(activity.reason(31, "reboot")).toContain("结果未知");
    });
    it("holds actions on a read failure and allows an explicit successful refresh", async () => {
        const { activity, api } = fixture();
        api.listMaintenanceOperations.mockRejectedValueOnce(new Error("network"));
        expect(await activity.refresh(31)).toBe(false);
        expect(activity.reason(31, "upgrade")).toContain("未能核查");
        expect(await activity.refresh(31)).toBe(true);
        expect(activity.reason(31, "upgrade")).toBe("");
    });
    it("ignores a read started before a newer POST result", async () => {
        const { activity, api } = fixture();
        let resolve!: (value: unknown) => void;
        api.listFirmwareUpgrades.mockReturnValueOnce(new Promise(r => { resolve = r; }));
        const pending = activity.refresh(31);
        activity.rememberUpgrade(31, upgrade("accepted"));
        resolve({ code: 0, data: { list: [upgrade("succeeded")] } });
        await pending;
        expect(activity.reason(31, "reboot")).toContain("升级");
    });
    it("restores remote upgrade and reboot locks without an open dialog", async () => {
        const { activity, api } = fixture();
        api.listFirmwareUpgrades.mockResolvedValue({ code: 0, data: { list: [upgrade("unknown")] } });
        api.listMaintenanceOperations.mockResolvedValue({ code: 0, data: { list: [{ operationId: "r1", status: "sent", responseRequired: true }] } });
        await activity.refresh(31);
        expect(activity.reason(31, "reboot")).toContain("升级");
        expect(activity.reason(31, "upgrade")).toContain("重启");
    });
    it("does not call a sent reboot with no required response a running task", () => {
        const { activity } = fixture();
        activity.rememberReboot(31, { operationId: "r1", action: "teleboot", status: "sent", responseRequired: false } as DeviceOperationResult);
        expect(activity.reason(31, "upgrade")).toBe("");
    });
});
