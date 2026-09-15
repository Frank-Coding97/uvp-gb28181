import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
const fetchDevices = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));
vi.mock("@/views/gb28181/device-mgmt/api", () => ({ listDevices: fetchDevices }));

import {
    applyPermissionWorkbenchAssignments,
    applyPermissionWorkbenchDepartmentAssignment,
    applyPermissionWorkbenchGrants,
    getPermissionWorkbenchSummary,
    listAssignmentDevices,
    queryPermissionWorkbenchGrants,
    resolvePermissionWorkbenchDevices,
    searchPermissionWorkbenchGrantTargets
} from "./api";

describe("device permission workbench API contract", () => {
    beforeEach(() => {
        request.mockReset();
        fetchDevices.mockReset();
    });

    it("forwards assignment and owner filters to the real device list", async () => {
        const params = {
            page: 2,
            pageSize: 50,
            q: "camera",
            status: "online" as const,
            assignment: "assigned" as const,
            ownerDeptId: 12
        };
        await listAssignmentDevices(params);
        expect(fetchDevices).toHaveBeenLastCalledWith(params);
    });

    it("uses summary and resolve endpoints", async () => {
        await getPermissionWorkbenchSummary();
        expect(request).toHaveBeenLastCalledWith(
            "get",
            "/api/gb28181/device-mgmt/permission-workbench/summary"
        );

        await resolvePermissionWorkbenchDevices([3, 7]);
        expect(request).toHaveBeenLastCalledWith(
            "post",
            "/api/gb28181/device-mgmt/permission-workbench/devices/resolve",
            { data: { deviceIds: [3, 7] } }
        );
    });

    it("submits item and department assignment concurrency fields", async () => {
        const items = [{ deviceId: 3, expectedOwnerDeptId: 10 }];
        await applyPermissionWorkbenchAssignments({ items, targetDeptId: 20 });
        expect(request).toHaveBeenLastCalledWith(
            "post",
            "/api/gb28181/device-mgmt/permission-workbench/assignments",
            { data: { items, targetDeptId: 20 } }
        );

        const departmentRequest = {
            sourceDeptId: 10,
            targetDeptId: 20,
            includeChildren: true,
            expectedCount: 8
        };
        await applyPermissionWorkbenchDepartmentAssignment(departmentRequest);
        expect(request).toHaveBeenLastCalledWith(
            "post",
            "/api/gb28181/device-mgmt/permission-workbench/assignments/departments",
            { data: departmentRequest }
        );
    });

    it("uses batch grants, revision apply, and paged target search", async () => {
        await queryPermissionWorkbenchGrants([3, 7]);
        expect(request).toHaveBeenLastCalledWith(
            "post",
            "/api/gb28181/device-mgmt/permission-workbench/grants/query",
            { data: { deviceIds: [3, 7] } }
        );

        const applyRequest = {
            items: [{ deviceId: 3, expectedRevision: "rev-1" }],
            mode: "remove" as const,
            targets: [{ type: "user" as const, id: 9 }]
        };
        await applyPermissionWorkbenchGrants(applyRequest);
        expect(request).toHaveBeenLastCalledWith(
            "post",
            "/api/gb28181/device-mgmt/permission-workbench/grants/apply",
            { data: applyRequest }
        );

        await searchPermissionWorkbenchGrantTargets({
            type: "user",
            q: "guard",
            page: 2,
            pageSize: 50
        });
        expect(request).toHaveBeenLastCalledWith(
            "get",
            "/api/gb28181/device-mgmt/permission-workbench/grant-targets",
            { params: { type: "user", q: "guard", page: 2, pageSize: 50 } }
        );
    });
});
