import { describe, expect, it } from "vitest";
import {
    mockQueryDeviceRecords,
    mockRecordQueryOptions,
    resolveRecordQueryMockScenario,
    shouldUseRecordQueryMock,
    type RecordQueryMockScenario
} from "./recordQueryMock";

describe("record query development mock", () => {
    it("enables mock only for explicit development mode", () => {
        expect(shouldUseRecordQueryMock({ DEV: true, VITE_RECORD_QUERY_MOCK: "true" })).toBe(true);
        expect(shouldUseRecordQueryMock({ DEV: true, VITE_RECORD_QUERY_MOCK: true })).toBe(true);
        expect(shouldUseRecordQueryMock({ DEV: true, VITE_RECORD_QUERY_MOCK: "false" })).toBe(false);
        expect(shouldUseRecordQueryMock({ DEV: false, VITE_RECORD_QUERY_MOCK: "true" })).toBe(false);
    });

    it("reads a supported scenario from normal or hash query strings", () => {
        expect(resolveRecordQueryMockScenario({ search: "?recordQueryMock=partial", hash: "" } as Location)).toBe("partial");
        expect(resolveRecordQueryMockScenario({ search: "", hash: "#/gb28181/device-mgmt?recordQueryMock=offline" } as Location)).toBe("offline");
        expect(resolveRecordQueryMockScenario({ search: "?recordQueryMock=unknown", hash: "" } as Location)).toBe("complete");
    });

    it("returns platform context from the options fixture", async () => {
        const response = await mockRecordQueryOptions(31);
        expect(response.code).toBe(0);
        expect(response.data).toMatchObject({
            channel: { id: 31 },
            timezone: "Asia/Shanghai",
            maxRangeHours: 24,
            supportedTypes: ["all", "manual", "alarm"]
        });
    });

    it.each([
        ["complete", "complete", 6],
        ["empty", "empty", 0],
        ["partial", "partial", 3]
    ] as Array<[RecordQueryMockScenario, string, number]>)
    ("returns a contract-valid %s result", async (scenario, status, count) => {
        const response = await mockQueryDeviceRecords(31, {
            startTime: "2026-08-02T00:00:00",
            endTime: "2026-08-02T19:40:20",
            type: "all",
            secrecy: 0,
            recorderId: ""
        }, scenario);
        expect(response.code).toBe(0);
        expect(response.data.status).toBe(status);
        expect(response.data.list).toHaveLength(count);
    });

    it("rejects timeout, offline and generic failure with stable error codes", async () => {
        await expect(mockQueryDeviceRecords(31, {} as never, "timeout")).rejects.toMatchObject({ errorCode: "record_query_timeout" });
        await expect(mockQueryDeviceRecords(31, {} as never, "offline")).rejects.toMatchObject({ errorCode: "record_query_device_offline" });
        await expect(mockQueryDeviceRecords(31, {} as never, "error")).rejects.toMatchObject({ errorCode: "record_query_send_failed" });
    });

    it("honors AbortSignal before resolving", async () => {
        const controller = new AbortController();
        const promise = mockQueryDeviceRecords(31, {} as never, "complete", controller.signal);
        controller.abort();
        await expect(promise).rejects.toMatchObject({ name: "AbortError" });
    });
});
