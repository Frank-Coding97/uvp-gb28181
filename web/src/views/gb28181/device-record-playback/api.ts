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
      headers: { "Idempotency-Key": idempotencyKey }
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
      headers: { "Idempotency-Key": idempotencyKey }
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
