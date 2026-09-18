import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { listMaintenanceOperations, rebootDevice } from "./api";

describe("device maintenance API", () => {
    beforeEach(() => {
        request.mockReset().mockResolvedValue({ code: 0, message: "", data: {} });
    });

    it("posts a confirmed device-level reboot with the idempotency key", async () => {
        await rebootDevice(31, { confirmed: true, idempotencyKey: "reboot-key-31" });

        expect(request).toHaveBeenCalledWith(
            "post",
            "/api/gb28181/device-mgmt/device/31/reboot",
            { data: { confirmed: true, idempotencyKey: "reboot-key-31" } }
        );
    });

    it("loads paged maintenance operations without leaking extra fields", async () => {
        await listMaintenanceOperations(31, { page: 2, pageSize: 10 });

        expect(request).toHaveBeenCalledWith(
            "get",
            "/api/gb28181/device-mgmt/device/31/maintenance-operations",
            { params: { page: 2, pageSize: 10 } }
        );
    });
});
