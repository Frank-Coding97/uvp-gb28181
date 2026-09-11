import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { listFirmwareUpgrades, upgradeDeviceFirmware } from "./api";

describe("device firmware upgrade API", () => {
    beforeEach(() => {
        request.mockReset().mockResolvedValue({ code: 0, message: "", data: {} });
    });

    it("posts the confirmed target version, URL, manufacturer, and idempotency key", async () => {
        await upgradeDeviceFirmware(31, {
            confirmed: true,
            idempotencyKey: "upgrade-key-31",
            firmware: "V5.9.0",
            fileUrl: "http://192.0.2.50/firmware/V5.9.0.bin",
            manufacturer: "海康"
        });

        expect(request).toHaveBeenCalledWith(
            "post",
            "/api/gb28181/device-mgmt/device/31/firmware-upgrade",
            {
                data: {
                    confirmed: true,
                    idempotencyKey: "upgrade-key-31",
                    firmware: "V5.9.0",
                    fileUrl: "http://192.0.2.50/firmware/V5.9.0.bin",
                    manufacturer: "海康"
                }
            }
        );
    });

    it("loads paged firmware upgrade history", async () => {
        await listFirmwareUpgrades(31, { page: 2, pageSize: 10 });

        expect(request).toHaveBeenCalledWith(
            "get",
            "/api/gb28181/device-mgmt/device/31/firmware-upgrades",
            { params: { page: 2, pageSize: 10 } }
        );
    });
});
