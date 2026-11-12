import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";
import type { PlaybackProtocol } from "@/api/gb28181";

export type PlaybackSessionState = "creating" | "buffering" | "playing" | "paused" | "ended" | "failed" | "stopping" | "stopped";

export interface PlaybackMediaUrls {
  wsFlv?: string;
  httpFlv?: string;
  hls?: string;
  webrtc?: string;
  wssFlv?: string;
  httpsFlv?: string;
  httpsHls?: string;
  webrtcs?: string;
  rtmp?: string;
  rtsp?: string;
}

export interface PlaybackSession {
  sessionId: string;
  state: PlaybackSessionState;
  channelId: string;
  recordKey: string;
  segmentStart: string;
  segmentEnd: string;
  positionSeconds: number;
  scale: number;
  hasAudio: boolean;
  media: {
    urls: PlaybackMediaUrls;
    defaultProtocol?: PlaybackProtocol;
    protocol?: PlaybackProtocol;
    url?: string;
    zlmWebrtc?: boolean;
  };
  expiresAt: string;
  errorStage: string;
  errorCode: string;
}

export interface CreatePlaybackSessionRequest {
  recordKey: string;
  playFrom: string;
}

export interface CreateDownloadSessionRequest {
  recordKey: string;
  playFrom: string;
  downloadSpeed: 1 | 2 | 4 | 8;
}

export type PlaybackActionRequest =
  | { action: "pause" | "resume" }
  | { action: "seek"; positionSeconds: number }
  | { action: "scale"; scale: number };

/**
 * 请求级开关。
 *
 * `utils/http` 对**每一个**失败请求都会自动弹一条 `Message.error(响应体 message)`,
 * 所以重试循环(等通道释放、探测会话是否已消失)里的中间失败必须显式抑制,
 * 否则用户会被连续弹出的「当前通道已有回放会话」糊一脸 —— 而且这些失败
 * 往往并不是最终结果。真正失败时的提示由调用方统一给一次。
 */
export interface PlaybackRequestOptions {
  showErrorMessage?: boolean;
}

const defaultRequestOptions: PlaybackRequestOptions = {};

/**
 * 建会话的请求超时，必须长于服务端的「收流超时」判定。
 *
 * 服务端要等设备把回放媒体送上来（`gb28181.playback.media_wait_sec`，默认 15 秒），
 * 等不到就判收流超时并当场拆会话，再返回 504。全局 15 秒会**同时**超时 ⇒ 浏览器
 * 先断开，用户看到的是笼统的网络错误，真实原因（收流超时）永远到不了前端。
 * 所以这两个接口单独放宽到 30 秒（15 秒等待 + 拆除与响应余量）。
 */
const HISTORICAL_SESSION_TIMEOUT_MS = 30_000;

function sessionCollectionPath(channelId: number) {
  return `gb28181/device-mgmt/channel/${channelId}/playback-sessions`;
}

function sessionPath(channelId: number, sessionId: string) {
  return `${sessionCollectionPath(channelId)}/${encodeURIComponent(sessionId)}`;
}

export function createPlaybackSession(
  channelId: number,
  data: CreatePlaybackSessionRequest,
  idempotencyKey: string,
  options: PlaybackRequestOptions = defaultRequestOptions
) {
  return http.request<BaseResult<PlaybackSession>>(
    "post",
    baseUrlApi(sessionCollectionPath(channelId)),
    {
      data,
      headers: { "Idempotency-Key": idempotencyKey },
      timeout: HISTORICAL_SESSION_TIMEOUT_MS
    },
    options
  );
}

export function createDownloadSession(channelId: number, data: CreateDownloadSessionRequest, idempotencyKey: string) {
  return http.request<BaseResult<PlaybackSession>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/channel/${channelId}/download-sessions`),
    {
      data,
      headers: { "Idempotency-Key": idempotencyKey },
      timeout: HISTORICAL_SESSION_TIMEOUT_MS
    }
  );
}

export function getPlaybackSession(
  channelId: number,
  sessionId: string,
  options: PlaybackRequestOptions = defaultRequestOptions
) {
  return http.request<BaseResult<PlaybackSession>>("get", baseUrlApi(sessionPath(channelId, sessionId)), undefined, options);
}

export function actionPlaybackSession(channelId: number, sessionId: string, data: PlaybackActionRequest) {
  return http.request<BaseResult<PlaybackSession>>("post", baseUrlApi(`${sessionPath(channelId, sessionId)}/actions`), { data });
}

export function deletePlaybackSession(
  channelId: number,
  sessionId: string,
  options: PlaybackRequestOptions = defaultRequestOptions
) {
  return http.request<BaseResult<{ state: "stopped" }>>(
    "delete",
    baseUrlApi(sessionPath(channelId, sessionId)),
    undefined,
    options
  );
}

export function selectPlaybackMediaUrl(urls: PlaybackMediaUrls | null | undefined) {
  return urls?.wsFlv || urls?.httpFlv || urls?.hls || "";
}

export function selectDownloadMediaUrl(urls: PlaybackMediaUrls | null | undefined) {
  return window.location.protocol === "https:" ? urls?.httpsFlv || urls?.httpFlv || "" : urls?.httpFlv || urls?.httpsFlv || "";
}
