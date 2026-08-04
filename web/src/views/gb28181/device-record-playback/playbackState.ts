export type PlaybackStatus = "unselected" | "selected" | "creating" | "buffering" | "playing" | "paused" | "ended" | "failed" | "stopping" | "stopped";

export interface PlaybackState {
    status: PlaybackStatus;
    recordKey: string | null;
    sessionId: string | null;
    currentTime: string | null;
    scale: number;
    error: string | null;
}

export type PlaybackEvent =
    | { type: "select"; recordKey: string }
    | { type: "creating" }
    | { type: "session"; sessionId: string; status: PlaybackStatus; currentTime?: string; message?: string }
    | { type: "pause" }
    | { type: "resume" }
    | { type: "scale"; scale: number }
    | { type: "stopping" }
    | { type: "stopped" };

export function createPlaybackState(): PlaybackState {
    return { status: "unselected", recordKey: null, sessionId: null, currentTime: null, scale: 1, error: null };
}

export function reducePlaybackState(state: PlaybackState, event: PlaybackEvent): PlaybackState {
    if (event.type === "select") return { ...createPlaybackState(), status: "selected", recordKey: event.recordKey };
    if (event.type === "creating") return state.recordKey ? { ...state, status: "creating", sessionId: null, error: null } : state;
    if (event.type === "session") {
        if (state.sessionId && state.sessionId !== event.sessionId) return state;
        return { ...state, sessionId: event.sessionId, status: event.status, currentTime: event.currentTime || state.currentTime, error: event.status === "failed" ? event.message || "回放失败" : null };
    }
    if (event.type === "pause") return state.status === "playing" ? { ...state, status: "paused" } : state;
    if (event.type === "resume") return ["paused", "buffering"].includes(state.status) ? { ...state, status: "playing" } : state;
    if (event.type === "scale") return { ...state, scale: event.scale };
    if (event.type === "stopping") return { ...state, status: "stopping" };
    return { ...state, status: "stopped", sessionId: null, currentTime: null, error: null };
}
