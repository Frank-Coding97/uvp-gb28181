import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";
import { listDevices as fetchDevices } from "@/views/gb28181/device-mgmt/api";
import type { DevicePageResult, OnlineStatus } from "@/views/gb28181/device-mgmt/api";

export type GrantTargetType = "dept" | "user";
export type AssignmentFilter = "all" | "unassigned" | "assigned";
export type PermissionApplyMode = "add" | "remove";

export interface GrantTarget { type: GrantTargetType; id: number; }
export interface GrantItem { id: number; targetType: GrantTargetType; targetId: number; targetName: string; invalid: boolean; }
export interface DeviceGrantState { deviceId: number; deviceCode: string; name: string; revision: string; grants: GrantItem[]; }
export interface GrantQueryResult { devices: DeviceGrantState[]; unavailableIds: number[]; }
export interface GrantTargetOption { id: number; type: GrantTargetType; name: string; deptId?: number; deptName?: string; }
export interface GrantTargetPage { list: GrantTargetOption[]; total: number; page: number; pageSize: number; }

export interface AssignmentItem { deviceId: number; expectedOwnerDeptId: number; }
export interface AssignmentResultItem {
    deviceId: number;
    deviceCode: string;
    name: string;
    status: "changed" | "skipped" | "failed";
    message: string;
}
export interface AssignmentResult {
    summary: { requested: number; changed: number; skipped: number; failed: number };
    results: AssignmentResultItem[];
}
export interface DepartmentAssignmentRequest { sourceDeptId: number; targetDeptId: number; includeChildren: boolean; expectedCount: number; }
export interface GrantApplyRequest {
    items: Array<{ deviceId: number; expectedRevision: string }>;
    mode: PermissionApplyMode;
    targets: GrantTarget[];
}
export interface GrantApplyResult {
    summary: { requested: number; changed: number; skipped: number; failed: number; added: number; removed: number; relationsSkipped: number };
    results: Array<{ deviceId: number; deviceCode: string; name: string; status: "changed" | "skipped" | "failed"; message: string; added: number; removed: number; skipped: number; revision: string }>;
}
export interface WorkbenchSummary {
    allCount: number;
    assignedCount: number;
    unassignedCount: number;
    departments: Array<{ deptId: number; directCount: number; subtreeCount: number }>;
}

export interface AssignmentListParams {
    page: number;
    pageSize: number;
    q?: string;
    status?: OnlineStatus;
    assignment: AssignmentFilter;
    ownerDeptId?: number;
}

export const listAssignmentDevices = (params: AssignmentListParams): Promise<BaseResult<DevicePageResult>> => fetchDevices(params);

export const getPermissionWorkbenchSummary = () =>
    http.request<BaseResult<WorkbenchSummary>>("get", baseUrlApi("gb28181/device-mgmt/permission-workbench/summary"));

export const resolvePermissionWorkbenchDevices = (deviceIds: number[]) =>
    http.request<BaseResult<{ devices: Array<{ id: number; deviceId: string; name: string; status: number; online: boolean; ownerDeptId: number; ownerDeptName: string }>; unavailableIds: number[] }>>(
        "post", baseUrlApi("gb28181/device-mgmt/permission-workbench/devices/resolve"), { data: { deviceIds } }
    );

export const applyPermissionWorkbenchAssignments = (data: { items: AssignmentItem[]; targetDeptId: number }) =>
    http.request<BaseResult<AssignmentResult>>("post", baseUrlApi("gb28181/device-mgmt/permission-workbench/assignments"), { data });

export const applyPermissionWorkbenchDepartmentAssignment = (data: DepartmentAssignmentRequest) =>
    http.request<BaseResult<AssignmentResult>>("post", baseUrlApi("gb28181/device-mgmt/permission-workbench/assignments/departments"), { data });

export const queryPermissionWorkbenchGrants = (deviceIds: number[]) =>
    http.request<BaseResult<GrantQueryResult>>("post", baseUrlApi("gb28181/device-mgmt/permission-workbench/grants/query"), { data: { deviceIds } });

export const applyPermissionWorkbenchGrants = (data: GrantApplyRequest) =>
    http.request<BaseResult<GrantApplyResult>>("post", baseUrlApi("gb28181/device-mgmt/permission-workbench/grants/apply"), { data });

export const searchPermissionWorkbenchGrantTargets = (params: { type: GrantTargetType; q?: string; page?: number; pageSize?: number }) =>
    http.request<BaseResult<GrantTargetPage>>("get", baseUrlApi("gb28181/device-mgmt/permission-workbench/grant-targets"), { params });
