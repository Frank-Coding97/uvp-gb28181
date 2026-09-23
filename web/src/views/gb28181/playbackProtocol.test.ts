import { describe, expect, it } from "vitest";
import {
    DEFAULT_PLAYBACK_PROTOCOL,
    normalizePlaybackProtocol,
    playbackProtocolOptionsFromDictionary,
    resolvePlaybackSource,
    selectPlaybackSource
} from "./playbackProtocol";

describe("playback protocol selection", () => {
    it("normalizes missing and invalid values to ws-flv", () => {
        expect(normalizePlaybackProtocol(undefined)).toBe(DEFAULT_PLAYBACK_PROTOCOL);
        expect(normalizePlaybackProtocol("RTMP")).toBe(DEFAULT_PLAYBACK_PROTOCOL);
        expect(normalizePlaybackProtocol("webrtc")).toBe("webrtc");
    });

    it("selects secure equivalents and uses a stable fallback", () => {
        const urls = {
            wsFlv: "ws://node/live.flv",
            wssFlv: "wss://node/live.flv",
            httpFlv: "http://node/live.flv",
            httpsFlv: "https://node/live.flv",
            hls: "http://node/hls.m3u8",
            httpsHls: "https://node/hls.m3u8"
        };
        expect(selectPlaybackSource(urls, "http-flv", true)).toEqual({
            protocol: "http-flv", url: "https://node/live.flv", zlmWebrtc: false
        });
        expect(selectPlaybackSource({ wssFlv: "wss://node/live.flv" }, "webrtc", true)).toEqual({
            protocol: "ws-flv", url: "wss://node/live.flv", zlmWebrtc: false
        });
        expect(selectPlaybackSource({ wsFlv: "ws://node/live.flv" }, "ws-flv", true)).toBeNull();
    });

    it("converts ZLM WebRTC snapshots for EasyPlayer", () => {
        expect(selectPlaybackSource({ webrtcs: "https://node/index/api/webrtc?app=rtp" }, "webrtc", true)).toEqual({
            protocol: "webrtc", url: "webrtc://node/index/api/webrtc?app=rtp", zlmWebrtc: true
        });
        expect(resolvePlaybackSource({
            defaultProtocol: "webrtc",
            protocol: "webrtc",
            url: "https://node/index/api/webrtc?app=rtp",
            zlmWebrtc: true
        }, true)).toEqual({
            protocol: "webrtc", url: "webrtc://node/index/api/webrtc?app=rtp", zlmWebrtc: true
        });
    });

    it("falls back for old responses without snapshot fields", () => {
        expect(resolvePlaybackSource({ urls: { httpFlv: "http://node/live.flv" } }, false)).toEqual({
            protocol: "http-flv", url: "http://node/live.flv", zlmWebrtc: false
        });
    });

    it("builds options from enabled supported dictionary items", () => {
        expect(playbackProtocolOptionsFromDictionary([
            { name: "WebRTC（低延迟）", value: "webrtc", status: 1 },
            { name: "未知协议", value: "rtmp", status: 1 },
            { name: "HLS（兼容）", value: "hls", status: 1 },
            { name: "重复 HLS", value: "hls", status: 1 },
            { name: "已禁用 HTTP-FLV", value: "http-flv", status: 0 },
            { name: "WS-FLV", value: "ws-flv", status: 1 }
        ])).toEqual([
            { label: "WebRTC（低延迟）", value: "webrtc" },
            { label: "HLS（兼容）", value: "hls" },
            { label: "WS-FLV", value: "ws-flv" }
        ]);
    });

    it("falls back to the built-in options when dictionary data is unavailable", () => {
        expect(playbackProtocolOptionsFromDictionary([])).toEqual([
            { label: "WS-FLV", value: "ws-flv" },
            { label: "HTTP-FLV", value: "http-flv" },
            { label: "HLS", value: "hls" },
            { label: "WebRTC", value: "webrtc" }
        ]);
    });
});
