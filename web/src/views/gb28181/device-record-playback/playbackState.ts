export type PlaybackStatus =
  | "unselected"
  | "selected"
  | "creating"
  | "buffering"
  | "playing"
  | "paused"
  | "ended"
  | "failed"
  | "stopping"
  | "stopped";

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
    return {
      ...state,
      sessionId: event.sessionId,
      status: event.status,
      currentTime: event.currentTime || state.currentTime,
      error: event.status === "failed" ? event.message || "回放失败" : null
    };
  }
  if (event.type === "pause") return state.status === "playing" ? { ...state, status: "paused" } : state;
  if (event.type === "resume") return ["paused", "buffering"].includes(state.status) ? { ...state, status: "playing" } : state;
  if (event.type === "scale") return { ...state, scale: event.scale };
  if (event.type === "stopping") return { ...state, status: "stopping" };
  return { ...state, status: "stopped", sessionId: null, currentTime: null, error: null };
}

/**
 * 解析回放路由里的通道参数。
 *
 * ⛔ 必须按字符串处理：20 位国标编码（`34020000001320000037`）超出
 * `Number.MAX_SAFE_INTEGER`，直接 `Number()` 会把末几位悄悄抹成 0，页面随后
 * 提示「通道不存在」——用户明明输的是对的编码，却极难自查。
 *
 * 返回值 0 表示「参数不可用」，调用方必须给出明确提示；**不要**回落成某个默认
 * 通道 id，那会把用户带到另一个通道的录像上。
 */
export function parsePlaybackChannelId(value: unknown): number {
  const raw = String(value ?? "").trim();
  if (!/^\d+$/.test(raw)) return 0;
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}
