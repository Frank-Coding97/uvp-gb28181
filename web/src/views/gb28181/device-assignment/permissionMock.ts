import type { BaseResult } from "@/api/types";
import type { DevicePageResult } from "@/views/gb28181/device-mgmt/api";
import type { AssignResult, GrantTarget, GrantVO } from "./api";

/**
 * 设备分配/共享授权的演示 mock。
 * 后端 T8(分配 API)/T9(共享 API)/T3(列表 ownerDeptId 过滤)完成后,
 * 将 shouldUsePermissionMock 改为返回 false,页面即自动切换到真实接口;
 * 届时本文件可整体删除。
 */

/** 演示用的默认部门 ID(真实环境由后端配置 gb28181.device.default_owner_dept_id 决定) */
const MOCK_DEFAULT_DEPT_ID = 1;

// 演示设备库(内存态,仅 tab 过滤视图使用;"全部"视图走真实接口)
interface MockDevice {
    id: number;
    deviceId: string;
    name: string;
    online: boolean;
    ownerDeptId: number;
    channelCount: number;
    channelOnlineCount: number;
    status: number;
}

const mockDeviceStore: MockDevice[] = [
    { id: 1, deviceId: "34020000001320000001", name: "园区 NVR-A", online: true, ownerDeptId: 2, channelCount: 8, channelOnlineCount: 8, status: 1 },
    { id: 2, deviceId: "34020000001320000002", name: "东门枪机-01", online: true, ownerDeptId: 2, channelCount: 1, channelOnlineCount: 1, status: 1 },
    { id: 3, deviceId: "34020000001320000003", name: "仓库球机-02", online: false, ownerDeptId: 3, channelCount: 1, channelOnlineCount: 0, status: 0 },
    { id: 4, deviceId: "34020000001320000004", name: "门店 A 半球-01", online: true, ownerDeptId: 3, channelCount: 1, channelOnlineCount: 1, status: 1 },
    { id: 5, deviceId: "34020000001320000005", name: "门店 A 半球-02", online: true, ownerDeptId: 3, channelCount: 1, channelOnlineCount: 1, status: 1 },
    { id: 6, deviceId: "34020000001320000006", name: "停车场枪机-01", online: true, ownerDeptId: MOCK_DEFAULT_DEPT_ID, channelCount: 1, channelOnlineCount: 1, status: 1 },
    { id: 7, deviceId: "34020000001320000007", name: "新到货 NVR-待分配", online: false, ownerDeptId: MOCK_DEFAULT_DEPT_ID, channelCount: 0, channelOnlineCount: 0, status: 0 },
    { id: 8, deviceId: "34020000001320000008", name: "演示枪机-待分配", online: true, ownerDeptId: MOCK_DEFAULT_DEPT_ID, channelCount: 1, channelOnlineCount: 1, status: 1 }
];

// 演示共享记录(内存态,key = deviceId)
const grantsByDevice = new Map<number, GrantVO[]>([
    [
        1,
        [
            {
                id: 1001,
                deviceId: 1,
                targetType: "dept",
                targetId: 4,
                targetName: "演示部门-安保部",
                createdBy: 1,
                createdAt: "2026-08-14 18:20:00"
            }
        ]
    ]
]);

let nextGrantId = 2000;

const ok = <T>(data: T): BaseResult<T> => ({ code: 0, message: "ok", data });

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/** DEV 环境默认走演示 mock;后端完成后改为 `return false`。 */
export function shouldUsePermissionMock(): boolean {
    return import.meta.env.DEV === true;
}

export interface MockListParams {
    page: number;
    pageSize: number;
    q?: string;
    status?: "online" | "offline";
    assignment: "all" | "unassigned" | "assigned";
    deptId?: number;
}

/** 分配状态视图的演示列表(tab 过滤 + 部门树过滤 + 关键词/状态) */
export async function mockListDevices(params: MockListParams): Promise<BaseResult<DevicePageResult>> {
    await sleep(280);
    let list = [...mockDeviceStore];
    if (params.assignment === "unassigned") {
        list = list.filter((d) => d.ownerDeptId === MOCK_DEFAULT_DEPT_ID);
    } else if (params.assignment === "assigned") {
        list = list.filter((d) => d.ownerDeptId !== MOCK_DEFAULT_DEPT_ID);
    }
    if (params.deptId !== undefined) {
        list = list.filter((d) => d.ownerDeptId === params.deptId);
    }
    if (params.q) {
        const q = params.q.trim();
        list = list.filter((d) => d.name.includes(q) || d.deviceId.includes(q));
    }
    if (params.status) {
        list = list.filter((d) => (params.status === "online" ? d.online : !d.online));
    }
    const total = list.length;
    const start = (params.page - 1) * params.pageSize;
    const pageList = list.slice(start, start + params.pageSize).map((d) => ({
        id: d.id,
        deviceId: d.deviceId,
        name: d.name,
        alias: "",
        transport: "UDP",
        manufacturer: "演示",
        model: "",
        firmware: "",
        ip: "10.0.0.1",
        port: 5060,
        status: d.status,
        online: d.online,
        channelCount: d.channelCount,
        channelOnlineCount: d.channelOnlineCount,
        onlineRate: d.channelCount ? Math.round((d.channelOnlineCount / d.channelCount) * 100) : 0,
        createdAt: "2026-08-10 10:00:00",
        updatedAt: "2026-08-14 10:00:00",
        ownerDeptId: d.ownerDeptId
    }));
    return ok({
        list: pageList,
        total,
        onlineTotal: list.filter((d) => d.online).length,
        offlineTotal: list.filter((d) => !d.online).length,
        page: params.page,
        pageSize: params.pageSize
    });
}

/** 批量勾选分配:更新内存设备库归属,演示分配后 tab 视图联动 */
export async function mockAssignDevices(deviceIds: number[], targetDeptId: number): Promise<BaseResult<AssignResult>> {
    await sleep(350);
    const results = deviceIds.map((deviceId, index) => {
        const device = mockDeviceStore.find((d) => d.id === deviceId);
        if (!device) {
            return { deviceId, success: false, message: "演示失败:设备不存在" };
        }
        if (index === 3) {
            return { deviceId, success: false, message: "演示失败:目标部门不存在" };
        }
        device.ownerDeptId = targetDeptId;
        return { deviceId, success: true, message: "ok" };
    });
    return ok({ results });
}

/** 整部门分配:流转该部门全部设备 */
export async function mockAssignDeptDevices(
    sourceDeptId: number,
    targetDeptId: number
): Promise<BaseResult<{ total: number; succeeded: number }>> {
    await sleep(400);
    const devices = mockDeviceStore.filter((d) => d.ownerDeptId === sourceDeptId);
    for (const device of devices) {
        device.ownerDeptId = targetDeptId;
    }
    return ok({ total: devices.length, succeeded: devices.length });
}

export async function mockListGrants(deviceId: number): Promise<BaseResult<GrantVO[]>> {
    await sleep(250);
    return ok(grantsByDevice.get(deviceId) ?? []);
}

export async function mockAddGrants(
    deviceId: number,
    targets: GrantTarget[]
): Promise<BaseResult<{ added: number; skipped: number }>> {
    await sleep(300);
    const existing = grantsByDevice.get(deviceId) ?? [];
    const keyOf = (t: GrantTarget) => `${t.type}:${t.id}`;
    const existingKeys = new Set(existing.map((g) => `${g.targetType}:${g.targetId}`));
    let added = 0;
    let skipped = 0;
    for (const target of targets) {
        if (existingKeys.has(keyOf(target))) {
            skipped += 1;
            continue;
        }
        const grant: GrantVO = {
            id: nextGrantId++,
            deviceId,
            targetType: target.type,
            targetId: target.id,
            targetName: target.name ?? (target.type === "dept" ? `部门 #${target.id}` : `用户 #${target.id}`),
            createdBy: 1,
            createdAt: "2026-08-15 10:00:00"
        };
        existing.push(grant);
        existingKeys.add(keyOf(target));
        added += 1;
    }
    grantsByDevice.set(deviceId, existing);
    return ok({ added, skipped });
}

export async function mockRemoveGrant(deviceId: number, grantId: number): Promise<BaseResult<null>> {
    await sleep(250);
    const existing = grantsByDevice.get(deviceId) ?? [];
    grantsByDevice.set(
        deviceId,
        existing.filter((g) => g.id !== grantId)
    );
    return ok(null);
}
