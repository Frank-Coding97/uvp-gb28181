import { describe, expect, it } from "vitest";
import { createPlaybackState, reducePlaybackState } from "./playbackState";

describe("record playback state", () => {
    it("selects a record without starting playback", () => {
        const selected = reducePlaybackState(createPlaybackState(), { type: "select", recordKey: "record-a" });
        expect(selected.status).toBe("selected");
        expect(selected.recordKey).toBe("record-a");
        expect(selected.sessionId).toBeNull();
    });

    it("ignores events from a replaced session", () => {
        const playing = reducePlaybackState({
            ...createPlaybackState(), status: "playing", recordKey: "record-b", sessionId: "session-new"
        }, { type: "session", sessionId: "session-old", status: "failed", message: "old failure" });
        expect(playing.status).toBe("playing");
        expect(playing.error).toBeNull();
    });

    it("keeps the selected record after a user stop", () => {
        const stopped = reducePlaybackState({
            ...createPlaybackState(), status: "playing", recordKey: "record-a", sessionId: "session-a"
        }, { type: "stopped" });
        expect(stopped).toMatchObject({ status: "stopped", recordKey: "record-a", sessionId: null });
    });
});
