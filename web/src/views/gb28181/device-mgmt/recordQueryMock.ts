import type { BaseResult } from "@/api/types";
import type { RecordQueryOptions, RecordQueryRequest, RecordQueryResult } from "./api";

export type RecordQueryMockScenario = "complete" | "empty" | "partial" | "timeout" | "offline" | "error";

export function shouldUseRecordQueryMock(_env: { DEV?: boolean; VITE_RECORD_QUERY_MOCK?: string | boolean }): boolean {
    return _env.DEV === true && (_env.VITE_RECORD_QUERY_MOCK === true || _env.VITE_RECORD_QUERY_MOCK === "true");
}

export function resolveRecordQueryMockScenario(_location?: Pick<Location, "search" | "hash">): RecordQueryMockScenario {
    const supported = new Set<RecordQueryMockScenario>(["complete", "empty", "partial", "timeout", "offline", "error"]);
    const location = _location || (typeof window !== "undefined" ? window.location : undefined);
    if (!location) return "complete";
    const direct = new URLSearchParams(location.search).get("recordQueryMock");
    const hashQuery = location.hash.includes("?") ? location.hash.slice(location.hash.indexOf("?")) : "";
    const fromHash = new URLSearchParams(hashQuery).get("recordQueryMock");
    const scenario = (direct || fromHash) as RecordQueryMockScenario | null;
    return scenario && supported.has(scenario) ? scenario : "complete";
}

export async function mockRecordQueryOptions(_channelId: number): Promise<BaseResult<RecordQueryOptions>> {
    const channel = mockChannel(_channelId);
    return {
        code: 0,
        message: "ok",
        data: {
            device: { id: 7, code: "34020000002000000001", name: "园区 NVR-A", online: true },
            channel,
            timezone: "Asia/Shanghai",
            serverNow: "2026-08-02T19:40:20+08:00",
            maxRangeHours: 24,
            timeoutSeconds: 15,
            supportedTypes: ["all", "manual", "alarm"]
        }
    };
}

export async function mockQueryDeviceRecords(
    _channelId: number,
    _request: RecordQueryRequest,
    _scenario: RecordQueryMockScenario,
    _signal?: AbortSignal
): Promise<BaseResult<RecordQueryResult>> {
    await waitForMock(_scenario === "timeout" ? 700 : 280, _signal);
    const errors = {
        timeout: { errorCode: "record_query_timeout", message: "设备在查询时限内未返回录像目录" },
        offline: { errorCode: "record_query_device_offline", message: "所属设备当前离线" },
        error: { errorCode: "record_query_send_failed", message: "录像查询指令发送失败" }
    } as const;
    if (_scenario in errors) throw errors[_scenario as keyof typeof errors];

    const allItems = mockItems(mockChannel(_channelId).code);
    const list = _scenario === "empty" ? [] : _scenario === "partial" ? allItems.slice(0, 3) : allItems;
    const status = _scenario === "empty" ? "empty" : _scenario === "partial" ? "partial" : "complete";
    return {
        code: 0,
        message: "ok",
        data: {
            status,
            partialReason: status === "partial" ? "deadline" : null,
            declaredTotal: status === "partial" ? 8 : list.length,
            receivedCount: list.length,
            incomplete: status === "partial",
            timezone: "Asia/Shanghai",
            elapsedMs: status === "partial" ? 15003 : 842,
            list
        }
    };
}

function waitForMock(delay: number, signal?: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
        if (signal?.aborted) {
            reject(new DOMException("Aborted", "AbortError"));
            return;
        }
        const timer = window.setTimeout(resolve, delay);
        signal?.addEventListener("abort", () => {
            window.clearTimeout(timer);
            reject(new DOMException("Aborted", "AbortError"));
        }, { once: true });
    });
}

function mockChannel(channelId: number) {
    if (channelId === 32) return { id: channelId, code: "34020000001320000002", name: "停车场通道" };
    return { id: channelId, code: "34020000001320000001", name: "东门出入口" };
}

function mockItems(channelCode: string) {
    const rows = [
        ["园区东门-上午巡检", "08:10:00", "08:42:16", "time", 248635904],
        ["园区东门-移动侦测", "09:03:25", "09:08:44", "alarm", 48902144],
        ["园区东门-手动留存", "10:18:00", "10:36:52", "manual", 161480704],
        ["园区东门-午间巡检", "12:00:00", "12:29:59", "time", 230686720],
        ["园区东门-访客告警", "14:21:07", "14:25:38", "alarm", 39845888],
        ["园区东门-下午巡检", "16:00:00", "16:42:13", "time", 356515840]
    ] as const;
    return rows.map(([name, start, end, type, fileSize], index) => ({
        deviceId: channelCode,
        name,
        filePath: `/record/20260802/${String(index + 1).padStart(3, "0")}.dav`,
        address: "园区东门",
        startTime: `2026-08-02T${start}+08:00`,
        endTime: `2026-08-02T${end}+08:00`,
        secrecy: 0,
        type,
        recorderId: "NVR-A",
        fileSize,
        recordLocation: "34020000002000000001",
        streamNumber: 0
    }));
}
