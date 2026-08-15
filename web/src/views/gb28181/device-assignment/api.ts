import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";
import { listDevices as fetchDevices } from "@/views/gb28181/device-mgmt/api";
import type { DevicePageResult, OnlineStatus } from "@/views/gb28181/device-mgmt/api";
import {
    shouldUsePermissionMock,
    mockAssignDevices,
    mockAssignDeptDevices,
    mockListDevices,
    mockListGrants,
    mockAddGrants,
    mockRemoveGrant,
    type MockListParams
} from "./permissionMock";

// ============ 类型 ============

export type GrantTargetType = "dept" | "user";

export interface GrantTarget {
    type: GrantTargetType;
    id: number;
    name?: string;
}

export interface GrantVO {
    id: number;
    deviceId: number;
    targetType: GrantTargetType;
    targetId: number;
    targetName: string;
    createdBy: number;
    createdAt: string;
}

export interface AssignItemResult {
    deviceId: number;
    success: boolean;
    message: string;
}

export interface AssignResult {
    results: AssignItemResult[];
}

// ============ 列表(带分配状态过滤) ============

export type AssignmentFilter = "all" | "unassigned" | "assigned";

export interface AssignmentListParams {
    page: number;
    pageSize: number;
    q?: string;
    status?: OnlineStatus;
    assignment: AssignmentFilter;
    /** 左侧部门树选中的部门(叠加过滤) */
    deptId?: number;
}

/**
 * 设备列表(带分配状态过滤)。
 * - mock 阶段:非"全部"视图走演示数据;
 * - TODO(后端 T3 完成后):mock 开关改 false,assignment/deptId 参数由后端解析(ownerDeptId)。
 */
export const listAssignmentDevices = async (params: AssignmentListParams): Promise<BaseResult<DevicePageResult>> => {
    if (shouldUsePermissionMock() && params.assignment !== "all") {
        return mockListDevices(params as MockListParams);
    }
    return fetchDevices({
        page: params.page,
        pageSize: params.pageSize,
        q: params.q,
        status: params.status,
        assignment: params.assignment,
        ownerDeptId: params.deptId
    } as never);
};

// ============ 归属分配 ============

/**
 * 分配设备归属部门(批量,逐条事务)。
 * TODO(后端 T8 完成后):mock 开关改为 false,走 http 真实调用。
 */
export const assignDevices = async (deviceIds: number[], targetDeptId: number): Promise<BaseResult<AssignResult>> => {
    if (shouldUsePermissionMock()) {
        return mockAssignDevices(deviceIds, targetDeptId);
    }
    return http.request<BaseResult<AssignResult>>("post", baseUrlApi("gb28181/device-mgmt/assign"), {
        data: { deviceIds, targetDeptId }
    });
};

/**
 * 整部门分配:流转该部门全部设备到目标部门。
 * TODO(后端 T8 完成后):mock 开关改为 false,走 http 真实调用。
 */
export const assignDeptDevices = async (
    sourceDeptId: number,
    targetDeptId: number
): Promise<BaseResult<{ total: number; succeeded: number }>> => {
    if (shouldUsePermissionMock()) {
        return mockAssignDeptDevices(sourceDeptId, targetDeptId);
    }
    return http.request<BaseResult<{ total: number; succeeded: number }>>(
        "post",
        baseUrlApi("gb28181/device-mgmt/assign-dept"),
        { data: { sourceDeptId, targetDeptId } }
    );
};

// ============ 共享授权 ============

/**
 * 查询某台设备的共享授权列表。
 * TODO(后端 T9 完成后):mock 开关改为 false,走 http 真实调用。
 */
export const listGrants = async (deviceId: number): Promise<BaseResult<GrantVO[]>> => {
    if (shouldUsePermissionMock()) {
        return mockListGrants(deviceId);
    }
    return http.request<BaseResult<GrantVO[]>>("get", baseUrlApi(`gb28181/device-mgmt/device/${deviceId}/grants`));
};

/**
 * 批量添加共享授权(设备 × 目标列表,逐条事务)。
 * TODO(后端 T9 完成后):mock 开关改为 false,走 http 真实调用。
 */
export const addGrants = async (
    deviceId: number,
    targets: GrantTarget[]
): Promise<BaseResult<{ added: number; skipped: number }>> => {
    if (shouldUsePermissionMock()) {
        return mockAddGrants(deviceId, targets);
    }
    return http.request<BaseResult<{ added: number; skipped: number }>>(
        "post",
        baseUrlApi(`gb28181/device-mgmt/device/${deviceId}/grants`),
        { data: { targets } }
    );
};

/**
 * 取消一条共享授权。
 * TODO(后端 T9 完成后):mock 开关改为 false,走 http 真实调用。
 */
export const removeGrant = async (deviceId: number, grantId: number): Promise<BaseResult<null>> => {
    if (shouldUsePermissionMock()) {
        return mockRemoveGrant(deviceId, grantId);
    }
    return http.request<BaseResult<null>>("delete", baseUrlApi(`gb28181/device-mgmt/device/${deviceId}/grants/${grantId}`));
};
