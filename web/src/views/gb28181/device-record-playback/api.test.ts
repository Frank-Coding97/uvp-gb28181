import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
    actionPlaybackSession,
    createPlaybackSession,
    deletePlaybackSession,
    getPlaybackSession,
    selectPlaybackMediaUrl
} from "./api";

describe("device record playback API", () => {
    beforeEach(() => request.mockResolvedValue({ code: 0, data: {} }));

    it("creates a playback session with the opaque record key and idempotency header", async () => {
        await createPlaybackSession(31, {
            recordKey: "opaque-record-key",
            playFrom: "2026-08-02T08:10:00+08:00"
        }, "playback-create-key");

        expect(request).toHaveBeenCalledWith(
            "post",
            "/api/gb28181/device-mgmt/channel/31/playback-sessions",
            {
                data: { recordKey: "opaque-record-key", playFrom: "2026-08-02T08:10:00+08:00" },
                headers: { "Idempotency-Key": "playback-create-key" }
            }
        );
    });

    it("loads, controls, and deletes one playback session", async () => {
        await getPlaybackSession(31, "session-1");
        await actionPlaybackSession(31, "session-1", { action: "seek", positionSeconds: 42 });
        await deletePlaybackSession(31, "session-1");

        expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/device-mgmt/channel/31/playback-sessions/session-1");
        expect(request).toHaveBeenNthCalledWith(2, "post", "/api/gb28181/device-mgmt/channel/31/playback-sessions/session-1/actions", {
            data: { action: "seek", positionSeconds: 42 }
        });
        expect(request).toHaveBeenNthCalledWith(3, "delete", "/api/gb28181/device-mgmt/channel/31/playback-sessions/session-1");
    });

    it("prefers ws-flv, then http-flv, then hls", () => {
        expect(selectPlaybackMediaUrl({ wsFlv: "ws://zlm/stream.live.flv", httpFlv: "http://zlm/stream.live.flv", hls: "http://zlm/hls.m3u8" }))
            .toBe("ws://zlm/stream.live.flv");
        expect(selectPlaybackMediaUrl({ httpFlv: "http://zlm/stream.live.flv", hls: "http://zlm/hls.m3u8" }))
            .toBe("http://zlm/stream.live.flv");
        expect(selectPlaybackMediaUrl({ hls: "http://zlm/hls.m3u8" })).toBe("http://zlm/hls.m3u8");
        expect(selectPlaybackMediaUrl({})).toBe("");
    });
});
