import type { PlaybackProtocol, PlayResult } from "@/api/gb28181";

export type { PlaybackProtocol };

export const DEFAULT_PLAYBACK_PROTOCOL: PlaybackProtocol = "ws-flv";
export const PLAYBACK_PROTOCOL_DICT_CODE = "gb28181_playback_protocol";

export interface PlaybackProtocolOption {
    label: string;
    value: PlaybackProtocol;
}

const FALLBACK_PLAYBACK_PROTOCOL_OPTIONS: PlaybackProtocolOption[] = [
    { label: "WS-FLV", value: "ws-flv" },
    { label: "HTTP-FLV", value: "http-flv" },
    { label: "HLS", value: "hls" },
    { label: "WebRTC", value: "webrtc" }
];

export interface PlaybackURLSet {
    wsFlv?: string | null;
    httpFlv?: string | null;
    wssFlv?: string | null;
    httpsFlv?: string | null;
    hls?: string | null;
    httpsHls?: string | null;
    webrtc?: string | null;
    webrtcs?: string | null;
}

export interface PlaybackSource {
    protocol: PlaybackProtocol;
    url: string;
    zlmWebrtc: boolean;
}

export function isPlaybackProtocol(value: unknown): value is PlaybackProtocol {
    return value === "ws-flv" || value === "http-flv" || value === "hls" || value === "webrtc";
}

export function normalizePlaybackProtocol(value: unknown): PlaybackProtocol {
    return isPlaybackProtocol(value) ? value : DEFAULT_PLAYBACK_PROTOCOL;
}

export function playbackProtocolOptionsFromDictionary(
    items: Array<{ name?: unknown; value?: unknown; status?: unknown }> | null | undefined
): PlaybackProtocolOption[] {
    const seen = new Set<PlaybackProtocol>();
    const options: PlaybackProtocolOption[] = [];
    for (const item of items || []) {
        if (item.status !== 1 && item.status !== true) continue;
        if (!isPlaybackProtocol(item.value) || seen.has(item.value)) continue;
        const label = typeof item.name === "string" ? item.name.trim() : "";
        if (!label) continue;
        seen.add(item.value);
        options.push({ label, value: item.value });
    }
    return options.length > 0 ? options : FALLBACK_PLAYBACK_PROTOCOL_OPTIONS.map(option => ({ ...option }));
}

function easyPlayerURL(protocol: PlaybackProtocol, url: string) {
    return protocol === "webrtc" ? url.replace(/^https?:\/\//i, "webrtc://") : url;
}

function candidateURL(urls: PlaybackURLSet, protocol: PlaybackProtocol, secure: boolean) {
    if (protocol === "ws-flv") return secure ? urls.wssFlv : (urls.wsFlv || urls.wssFlv);
    if (protocol === "http-flv") return secure ? urls.httpsFlv : (urls.httpFlv || urls.httpsFlv);
    if (protocol === "hls") return secure ? urls.httpsHls : (urls.hls || urls.httpsHls);
    return secure ? urls.webrtcs : (urls.webrtc || urls.webrtcs);
}

export function selectPlaybackSource(
    urls: PlaybackURLSet | null | undefined,
    preferred: unknown,
    secure: boolean
): PlaybackSource | null {
    const normalized = normalizePlaybackProtocol(preferred);
    const order: PlaybackProtocol[] = [normalized, ...(["ws-flv", "http-flv", "hls", "webrtc"] as PlaybackProtocol[])
        .filter(protocol => protocol !== normalized)];
    for (const protocol of order) {
        const url = candidateURL(urls || {}, protocol, secure);
        if (url) return { protocol, url: easyPlayerURL(protocol, url), zlmWebrtc: protocol === "webrtc" };
    }
    return null;
}

function safeSnapshotURL(url: string, secure: boolean) {
    return !secure || /^(https|wss|webrtc):\/\//i.test(url);
}

export function resolvePlaybackSource(
    response: Pick<PlayResult, "defaultProtocol" | "protocol" | "url" | "zlmWebrtc" | "urls">,
    secure: boolean
): PlaybackSource | null {
    if (isPlaybackProtocol(response.protocol) && response.url && safeSnapshotURL(response.url, secure)) {
        return {
            protocol: response.protocol,
            url: easyPlayerURL(response.protocol, response.url),
            zlmWebrtc: response.protocol === "webrtc" && response.zlmWebrtc !== false
        };
    }
    return selectPlaybackSource(response.urls, response.defaultProtocol, secure);
}
