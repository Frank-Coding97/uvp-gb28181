import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
const getAccessToken = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/utils/auth", () => ({ getAccessToken }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
    buildTraceStreamUrl,
    fetchTraceHealth,
    fetchTraceSessionStats,
    getTraceMessage,
    listTraceMessages,
    listTraceSessionMessages,
    listTraceSessions
} from "./gb28181-trace";

describe("GB28181 SIP trace API contract", () => {
    beforeEach(() => {
        request.mockReset();
        request.mockResolvedValue({ code: 0, message: "", data: {} });
        getAccessToken.mockReset();
    });

    it("keeps message, session, stats and detail routes storage-agnostic", async () => {
        const messageQuery = {
            from: "2026-08-10T10:00:00.000Z",
            to: "2026-08-10T10:15:00.000Z",
            deviceIds: "device-a,device-b",
            direction: "inbound" as const,
            method: "REGISTER",
            callId: "call-a",
            statusMin: 400,
            statusMax: 499,
            cursor: "cursor-a",
            limit: 50
        };
        await fetchTraceHealth();
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/health");

        await listTraceMessages(messageQuery);
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/messages", { params: messageQuery });

        await getTraceMessage("event/1");
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/messages/event/1", {
            params: { sensitive: undefined, purpose: undefined }
        });

        const sessionQuery = {
            from: messageQuery.from,
            to: messageQuery.to,
            diagnosisCategory: "play_stuck" as const,
            diagnosisCode: "media_timeout" as const,
            limit: 200
        };
        await listTraceSessions(sessionQuery);
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/sessions", { params: sessionQuery });

        await fetchTraceSessionStats(sessionQuery);
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/sessions/stats", { params: sessionQuery });

        await listTraceSessionMessages("call/id", messageQuery);
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/sessions/call%2Fid/messages", {
            params: messageQuery
        });
    });

    it("preserves sensitive detail audit parameters", async () => {
        await getTraceMessage("event-a", { sensitive: true, purpose: "incident-42" });
        expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip-traces/messages/event-a", {
            params: { sensitive: "true", purpose: "incident-42" }
        });
    });

    it("builds the existing authenticated SSE URL without a storage selector", () => {
        getAccessToken.mockReturnValue({ accessToken: "token value" });

        const url = buildTraceStreamUrl({ deviceId: "device-a", callId: "call-a", method: "INVITE" });
        expect(url).toBe(
            "/api/gb28181/sip-traces/stream?token=token+value&deviceId=device-a&callId=call-a&method=INVITE"
        );
        expect(url).not.toContain("storage");
        expect(url).not.toContain("clickhouse");
    });
});
