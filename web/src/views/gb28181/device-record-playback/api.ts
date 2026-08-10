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

export type PlaybackActionRequest =
    | { action: "pause" | "resume" }
    | { action: "seek"; positionSeconds: number }
    | { action: "scale"; scale: number };

function sessionCollectionPath(channelId: number) {
    return `gb28181/device-mgmt/channel/${channelId}/playback-sessions`;
}

function sessionPath(channelId: number, sessionId: string) {
    return `${sessionCollectionPath(channelId)}/${encodeURIComponent(sessionId)}`;
}

export function createPlaybackSession(channelId: number, data: CreatePlaybackSessionRequest, idempotencyKey: string) {
    return http.request<BaseResult<PlaybackSession>>("post", baseUrlApi(sessionCollectionPath(channelId)), {
        data,
        headers: { "Idempotency-Key": idempotencyKey }
    });
}

export function getPlaybackSession(channelId: number, sessionId: string) {
    return http.request<BaseResult<PlaybackSession>>("get", baseUrlApi(sessionPath(channelId, sessionId)));
}

export function actionPlaybackSession(channelId: number, sessionId: string, data: PlaybackActionRequest) {
    return http.request<BaseResult<PlaybackSession>>("post", baseUrlApi(`${sessionPath(channelId, sessionId)}/actions`), { data });
}

export function deletePlaybackSession(channelId: number, sessionId: string) {
    return http.request<BaseResult<{ state: "stopped" }>>("delete", baseUrlApi(sessionPath(channelId, sessionId)));
}

export function selectPlaybackMediaUrl(urls: PlaybackMediaUrls | null | undefined) {
    return urls?.wsFlv || urls?.httpFlv || urls?.hls || "";
}
