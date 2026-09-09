import { reactive } from "vue";
import {
    getWorkRecordingStatus,
    startWorkRecording,
    stopWorkRecording,
    type WorkRecordingSnapshot,
    type WorkRecordingState
} from "@/api/gb28181-work-recording";

export type WorkRecordingLoadState = "idle" | "loading" | "ready" | "error";
export type WorkRecordingAction = "start" | "stop";

type ApiResponse<T> = { code: number; data: T; message: string };

const startableStates: WorkRecordingState[] = ["idle", "stopped", "failed"];

function newRequestId() {
    if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
    return `work-recording-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function responseData<T>(response: ApiResponse<T>) {
    if (!response || response.code !== 0) {
        const reason = new Error(response?.message || "作业录像请求失败") as Error & { snapshot?: T };
        reason.snapshot = response?.data;
        throw reason;
    }
    return response.data;
}

function uncertain(reason: any) {
    const code = String(reason?.code || "").toUpperCase();
    const message = String(reason?.message || "").toLowerCase();
    return code === "ECONNABORTED" || code === "ERR_NETWORK" || /timeout|network|fetch failed|超时|网络/.test(message);
}

function responseSnapshot(reason: any) {
    return reason?.snapshot || reason?.response?.data?.data;
}

export function useWorkRecording() {
    const snapshots = reactive(new Map<number, WorkRecordingSnapshot>());
    const loadStates = reactive(new Map<number, WorkRecordingLoadState>());
    const errors = reactive(new Map<number, string>());
    const actions = reactive(new Map<number, WorkRecordingAction>());
    const requestSequences = new Map<number, number>();
    const requestIds = new Map<number, string>();
    const inFlight = new Map<number, Promise<WorkRecordingSnapshot | null>>();

    function nextSequence(channelId: number) {
        const sequence = (requestSequences.get(channelId) || 0) + 1;
        requestSequences.set(channelId, sequence);
        return sequence;
    }

    function currentSequence(channelId: number) {
        return requestSequences.get(channelId) || 0;
    }

    function applySnapshot(next: WorkRecordingSnapshot, sequence = currentSequence(next.channelId)) {
        const current = snapshots.get(next.channelId);
        if (sequence !== currentSequence(next.channelId)) return false;
        const sameJob = Boolean(current?.id && next.id && current.id === next.id);
        if (sameJob && current && next.version < current.version) return false;
        snapshots.set(next.channelId, next);
        return true;
    }

    function snapshot(channelId: number) {
        return snapshots.get(channelId) || null;
    }

    function loadState(channelId: number) {
        return loadStates.get(channelId) || "idle";
    }

    function pendingAction(channelId: number) {
        return actions.get(channelId) || null;
    }

    function error(channelId: number) {
        return errors.get(channelId) || "";
    }

    function isReady(channelId: number) {
        return loadState(channelId) === "ready" && Boolean(snapshot(channelId));
    }

    function canStart(channelId: number) {
        const current = snapshot(channelId);
        return isReady(channelId) && !pendingAction(channelId) && Boolean(current && startableStates.includes(current.state));
    }

    function canStop(channelId: number) {
        const current = snapshot(channelId);
        return isReady(channelId) && !pendingAction(channelId) &&
            Boolean(current?.id) && (current?.state === "recording" || current?.state === "unknown");
    }

    async function refresh(channelIds: number[]) {
        const ids = [...new Set(channelIds.filter(channelId => Number.isSafeInteger(channelId) && channelId > 0))];
        if (!ids.length) return false;
        const sequences = new Map(ids.map(channelId => [channelId, nextSequence(channelId)]));
        ids.forEach(channelId => {
            loadStates.set(channelId, "loading");
            errors.delete(channelId);
        });
        try {
            const response = responseData(await getWorkRecordingStatus(ids));
            const byChannel = new Map((response || []).map(item => [item.channelId, item]));
            ids.forEach(channelId => {
                if (sequences.get(channelId) !== currentSequence(channelId)) return;
                const item = byChannel.get(channelId);
                if (!item) {
                    loadStates.set(channelId, "error");
                    errors.set(channelId, "服务端未返回该通道的录像状态");
                    return;
                }
                applySnapshot(item, sequences.get(channelId));
                loadStates.set(channelId, "ready");
            });
            return true;
        } catch (reason: any) {
            ids.forEach(channelId => {
                if (sequences.get(channelId) !== currentSequence(channelId)) return;
                loadStates.set(channelId, "error");
                errors.set(channelId, reason?.message || "查询录像状态失败");
            });
            return false;
        }
    }

    function ensureStatus(channelId: number) {
        if (loadState(channelId) === "idle") return refresh([channelId]);
        return Promise.resolve(loadState(channelId) === "ready");
    }

    function finishAction(channelId: number, action: WorkRecordingAction) {
        if (actions.get(channelId) === action) actions.delete(channelId);
    }

    function start(channelId: number) {
        const existing = inFlight.get(channelId);
        if (existing) return existing;
        if (!canStart(channelId)) return Promise.resolve(null);
        const sequence = nextSequence(channelId);
        const requestId = requestIds.get(channelId) || newRequestId();
        requestIds.set(channelId, requestId);
        actions.set(channelId, "start");
        const operation = (async () => {
            try {
                const response = responseData(await startWorkRecording({ channelId, requestId }));
                if (!response) throw new Error("服务器未返回录像状态");
                const applied = applySnapshot(response, sequence);
                if (applied) {
                    loadStates.set(channelId, "ready");
                    errors.delete(channelId);
                }
                if (response.state !== "unknown" && response.state !== "failed") requestIds.delete(channelId);
                return response;
            } catch (reason: any) {
                if (sequence === currentSequence(channelId)) {
                    const failedSnapshot = responseSnapshot(reason) as WorkRecordingSnapshot | undefined;
                    if (failedSnapshot?.channelId === channelId) {
                        applySnapshot(failedSnapshot, sequence);
                        loadStates.set(channelId, "ready");
                        if (failedSnapshot.state === "failed") requestIds.delete(channelId);
                    } else if (uncertain(reason)) {
                        const current = snapshot(channelId);
                        if (current) snapshots.set(channelId, { ...current, state: "unknown", lastError: reason?.message || "开始录像状态待核实" });
                        loadStates.set(channelId, "error");
                    }
                    errors.set(channelId, reason?.message || "开始录像失败");
                }
                return null;
            } finally {
                finishAction(channelId, "start");
                inFlight.delete(channelId);
            }
        })();
        inFlight.set(channelId, operation);
        return operation;
    }

    function stop(channelId: number) {
        const existing = inFlight.get(channelId);
        if (existing) return existing;
        const current = snapshot(channelId);
        if (!canStop(channelId) || !current?.id) return Promise.resolve(null);
        const sequence = nextSequence(channelId);
        actions.set(channelId, "stop");
        const operation = (async () => {
            try {
                const response = responseData(await stopWorkRecording(current.id));
                if (!response) throw new Error("服务器未返回录像状态");
                if (applySnapshot(response, sequence)) {
                    loadStates.set(channelId, "ready");
                    errors.delete(channelId);
                }
                return response;
            } catch (reason: any) {
                if (sequence === currentSequence(channelId)) {
                    const failedSnapshot = responseSnapshot(reason) as WorkRecordingSnapshot | undefined;
                    if (failedSnapshot?.channelId === channelId) {
                        applySnapshot(failedSnapshot, sequence);
                        loadStates.set(channelId, "ready");
                    } else if (uncertain(reason)) {
                        const current = snapshot(channelId);
                        if (current) snapshots.set(channelId, { ...current, state: "unknown", lastError: reason?.message || "结束录像状态待核实" });
                        loadStates.set(channelId, "error");
                    }
                    errors.set(channelId, reason?.message || "结束录像失败");
                }
                return null;
            } finally {
                finishAction(channelId, "stop");
                inFlight.delete(channelId);
            }
        })();
        inFlight.set(channelId, operation);
        return operation;
    }

    return {
        snapshot,
        loadState,
        pendingAction,
        error,
        isReady,
        canStart,
        canStop,
        ensureStatus,
        refresh,
        start,
        stop
    };
}
