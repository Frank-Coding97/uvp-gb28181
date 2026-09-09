import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    getWorkRecordingStatus: vi.fn(),
    startWorkRecording: vi.fn(),
    stopWorkRecording: vi.fn()
}));

vi.mock("@/api/gb28181-work-recording", () => api);

import type { WorkRecordingSnapshot } from "@/api/gb28181-work-recording";
import { useWorkRecording } from "./useWorkRecording";

function snapshot(channelId: number, state: WorkRecordingSnapshot["state"], version: number, id = "") {
    return {
        id,
        channelId,
        state,
        version,
        lastCheckedAt: null,
        startedAt: null,
        stoppedAt: null,
        fileState: "pending",
        formState: "draft",
        lastError: ""
    } satisfies WorkRecordingSnapshot;
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

describe("useWorkRecording", () => {
    beforeEach(() => {
        api.getWorkRecordingStatus.mockReset();
        api.startWorkRecording.mockReset();
        api.stopWorkRecording.mockReset();
    });

    it("does not allow start before the first status query succeeds", async () => {
        api.getWorkRecordingStatus.mockRejectedValueOnce(new Error("network down"));
        const recording = useWorkRecording();

        await recording.refresh([7]);

        expect(recording.loadState(7)).toBe("error");
        expect(recording.canStart(7)).toBe(false);
        expect(recording.canStop(7)).toBe(false);
        expect(recording.snapshot(7)).toBeNull();
        expect(api.startWorkRecording).not.toHaveBeenCalled();
        expect(api.stopWorkRecording).not.toHaveBeenCalled();
    });

    it("drops an older query result when a newer query has already applied", async () => {
        const older = deferred<{ code: number; data: WorkRecordingSnapshot[]; message: string }>();
        const newer = deferred<{ code: number; data: WorkRecordingSnapshot[]; message: string }>();
        api.getWorkRecordingStatus.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
        const recording = useWorkRecording();

        const oldRequest = recording.refresh([7]);
        const newRequest = recording.refresh([7]);
        newer.resolve({ code: 0, data: [snapshot(7, "recording", 4, "job-7")], message: "" });
        await newRequest;
        older.resolve({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        await oldRequest;

        expect(recording.snapshot(7)).toMatchObject({ state: "recording", version: 4, id: "job-7" });
        expect(recording.loadState(7)).toBe("ready");
    });

    it("does not act on a cached job while its latest status query has failed", async () => {
        api.getWorkRecordingStatus
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 5, "job-7")], message: "" })
            .mockRejectedValueOnce(new Error("network down"));
        const recording = useWorkRecording();

        await recording.refresh([7]);
        expect(recording.canStop(7)).toBe(true);
        await recording.refresh([7]);

        expect(recording.snapshot(7)).toMatchObject({ state: "recording", id: "job-7" });
        expect(recording.loadState(7)).toBe("error");
        expect(recording.canStart(7)).toBe(false);
        expect(recording.canStop(7)).toBe(false);
        expect(api.stopWorkRecording).not.toHaveBeenCalled();
    });

    it("coalesces a double click and reuses requestId for a failed retry", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        const firstStart = deferred<{ code: number; data: WorkRecordingSnapshot; message: string }>();
        api.startWorkRecording.mockReturnValueOnce(firstStart.promise).mockRejectedValueOnce(new Error("temporary failure"));
        const recording = useWorkRecording();
        await recording.refresh([7]);

        const first = recording.start(7);
        const duplicate = recording.start(7);
        expect(api.startWorkRecording).toHaveBeenCalledTimes(1);
        expect(api.startWorkRecording.mock.calls[0][0]).toEqual({ channelId: 7, requestId: expect.any(String) });
        expect(recording.pendingAction(7)).toBe("start");

        firstStart.reject(new Error("temporary failure"));
        await Promise.all([first, duplicate]);
        const firstRequestId = api.startWorkRecording.mock.calls[0][0].requestId;
        await recording.start(7);
        expect(api.startWorkRecording).toHaveBeenCalledTimes(2);
        expect(api.startWorkRecording.mock.calls[1][0].requestId).toBe(firstRequestId);
    });

    it("creates a fresh requestId for a new start after a confirmed stop", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        api.startWorkRecording
            .mockResolvedValueOnce({ code: 0, data: snapshot(7, "recording", 1, "job-1"), message: "" })
            .mockResolvedValueOnce({ code: 0, data: snapshot(7, "recording", 3, "job-2"), message: "" });
        api.stopWorkRecording.mockResolvedValueOnce({ code: 0, data: snapshot(7, "stopped", 2, "job-1"), message: "" });
        const recording = useWorkRecording();
        await recording.refresh([7]);
        await recording.start(7);
        await recording.stop(7);
        await recording.start(7);

        expect(api.startWorkRecording.mock.calls[1][0].requestId).not.toBe(api.startWorkRecording.mock.calls[0][0].requestId);
    });

    it("accepts a new job with a lower version after the previous job stops", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 8, "job-a")], message: "" });
        api.stopWorkRecording.mockResolvedValueOnce({ code: 0, data: snapshot(7, "stopped", 9, "job-a"), message: "" });
        api.startWorkRecording.mockResolvedValueOnce({ code: 0, data: snapshot(7, "recording", 3, "job-b"), message: "" });
        const recording = useWorkRecording();

        await recording.refresh([7]);
        await recording.stop(7);
        await recording.start(7);

        expect(recording.snapshot(7)).toMatchObject({ id: "job-b", state: "recording", version: 3 });
    });

    it("drops a late old-job status response after a newer job status", async () => {
        const oldStatus = deferred<{ code: number; data: WorkRecordingSnapshot[]; message: string }>();
        const newStatus = deferred<{ code: number; data: WorkRecordingSnapshot[]; message: string }>();
        api.getWorkRecordingStatus.mockReturnValueOnce(oldStatus.promise).mockReturnValueOnce(newStatus.promise);
        const recording = useWorkRecording();

        const oldRequest = recording.refresh([7]);
        const newRequest = recording.refresh([7]);
        newStatus.resolve({ code: 0, data: [snapshot(7, "recording", 3, "job-b")], message: "" });
        await newRequest;
        oldStatus.resolve({ code: 0, data: [snapshot(7, "recording", 9, "job-a")], message: "" });
        await oldRequest;

        expect(recording.snapshot(7)).toMatchObject({ id: "job-b", state: "recording", version: 3 });
    });

    it("keeps the start response effective when refresh is requested during the action", async () => {
        const startResponse = deferred<{ code: number; data: WorkRecordingSnapshot; message: string }>();
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        api.startWorkRecording.mockReturnValueOnce(startResponse.promise);
        const recording = useWorkRecording();
        await recording.refresh([7]);

        const start = recording.start(7);
        const refresh = recording.refresh([7]);
        expect(await refresh).toBe(false);
        expect(api.getWorkRecordingStatus).toHaveBeenCalledTimes(1);
        expect(recording.snapshot(7)).toMatchObject({ state: "idle", version: 0, id: "" });

        startResponse.resolve({ code: 0, data: snapshot(7, "recording", 1, "job-1"), message: "" });
        await start;

        expect(recording.snapshot(7)).toMatchObject({ state: "recording", version: 1, id: "job-1" });
        expect(recording.pendingAction(7)).toBeNull();
    });

    it("does not let a refresh overwrite the state while a stop action is pending", async () => {
        const stopResponse = deferred<{ code: number; data: WorkRecordingSnapshot; message: string }>();
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 5, "job-7")], message: "" });
        api.stopWorkRecording.mockReturnValueOnce(stopResponse.promise);
        const recording = useWorkRecording();
        await recording.refresh([7]);

        const stop = recording.stop(7);
        const refresh = recording.refresh([7]);
        expect(recording.pendingAction(7)).toBe("stop");
        expect(await refresh).toBe(false);
        expect(api.getWorkRecordingStatus).toHaveBeenCalledTimes(1);
        expect(recording.snapshot(7)).toMatchObject({ state: "recording", version: 5, id: "job-7" });

        stopResponse.resolve({ code: 0, data: snapshot(7, "stopped", 6, "job-7"), message: "" });
        await stop;
        expect(recording.snapshot(7)).toMatchObject({ state: "stopped", version: 6, id: "job-7" });
        expect(recording.pendingAction(7)).toBeNull();
    });

    it("prefers the server error message over Axios's generic request error", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        api.startWorkRecording.mockRejectedValueOnce(Object.assign(new Error("Request failed with status code 503"), {
            response: { data: { message: "录像服务暂不可用" } }
        }));
        const recording = useWorkRecording();
        await recording.refresh([7]);

        await recording.start(7);

        expect(recording.error(7)).toBe("录像服务暂不可用");
    });

    it("prefers the server error message when stopping fails", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 5, "job-7")], message: "" });
        api.stopWorkRecording.mockRejectedValueOnce(Object.assign(new Error("Request failed with status code 503"), {
            response: { data: { message: "录像服务拒绝停止" } }
        }));
        const recording = useWorkRecording();
        await recording.refresh([7]);

        await recording.stop(7);

        expect(recording.error(7)).toBe("录像服务拒绝停止");
    });

    it("allows a stop retry for a queried unknown job with a durable id", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "unknown", 5, "job-7")], message: "" });
        api.stopWorkRecording.mockResolvedValueOnce({ code: 0, data: snapshot(7, "stopped", 6, "job-7"), message: "" });
        const recording = useWorkRecording();
        await recording.refresh([7]);

        expect(recording.canStart(7)).toBe(false);
        expect(recording.canStop(7)).toBe(true);
        await recording.stop(7);
        expect(api.stopWorkRecording).toHaveBeenCalledWith("job-7");
        expect(recording.snapshot(7)?.state).toBe("stopped");
    });

    it("keeps a legacy unknown claim with no id non-actionable", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "unknown", 5)], message: "" });
        const recording = useWorkRecording();
        await recording.refresh([7]);

        expect(recording.canStart(7)).toBe(false);
        expect(recording.canStop(7)).toBe(false);
        await recording.stop(7);
        expect(api.stopWorkRecording).not.toHaveBeenCalled();
    });

    it("can recover a lost stop response by querying the durable unknown job", async () => {
        api.getWorkRecordingStatus
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 5, "job-7")], message: "" })
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "unknown", 6, "job-7")], message: "" });
        api.stopWorkRecording.mockRejectedValueOnce(Object.assign(new Error("request timeout"), { code: "ECONNABORTED" }));
        const recording = useWorkRecording();
        await recording.refresh([7]);

        await recording.stop(7);
        expect(recording.snapshot(7)).toMatchObject({ state: "unknown", id: "job-7" });
        expect(recording.canStop(7)).toBe(false);

        await recording.refresh([7]);
        expect(recording.snapshot(7)).toMatchObject({ state: "unknown", id: "job-7" });
        expect(recording.canStop(7)).toBe(true);
    });

    it("marks a started recording as unknown when the start response times out", async () => {
        api.getWorkRecordingStatus.mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" });
        const timeout = Object.assign(new Error("request timeout"), { code: "ECONNABORTED" });
        api.startWorkRecording.mockRejectedValueOnce(timeout);
        const recording = useWorkRecording();
        await recording.refresh([7]);

        await recording.start(7);

        expect(recording.snapshot(7)).toMatchObject({ state: "unknown", lastError: "request timeout" });
        expect(recording.canStart(7)).toBe(false);
        expect(recording.canStop(7)).toBe(false);
    });

    it("lets the server's idle version zero replace the previous job before a new round", async () => {
        api.getWorkRecordingStatus
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 5, "job-old")], message: "" })
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "idle", 0)], message: "" })
            .mockResolvedValueOnce({ code: 0, data: [snapshot(7, "recording", 1, "job-new")], message: "" });
        const recording = useWorkRecording();

        await recording.refresh([7]);
        await recording.refresh([7]);
        expect(recording.snapshot(7)).toMatchObject({ state: "idle", version: 0, id: "" });
        await recording.refresh([7]);
        expect(recording.snapshot(7)).toMatchObject({ state: "recording", version: 1, id: "job-new" });
    });
});
