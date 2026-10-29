import { describe, expect, it } from "vitest";
import { createPlaybackState, parsePlaybackChannelId, reducePlaybackState } from "./playbackState";

describe("record playback state", () => {
  it("selects a record without starting playback", () => {
    const selected = reducePlaybackState(createPlaybackState(), { type: "select", recordKey: "record-a" });
    expect(selected.status).toBe("selected");
    expect(selected.recordKey).toBe("record-a");
    expect(selected.sessionId).toBeNull();
  });

  it("ignores events from a replaced session", () => {
    const playing = reducePlaybackState(
      {
        ...createPlaybackState(),
        status: "playing",
        recordKey: "record-b",
        sessionId: "session-new"
      },
      { type: "session", sessionId: "session-old", status: "failed", message: "old failure" }
    );
    expect(playing.status).toBe("playing");
    expect(playing.error).toBeNull();
  });

  it("keeps the selected record after a user stop", () => {
    const stopped = reducePlaybackState(
      {
        ...createPlaybackState(),
        status: "playing",
        recordKey: "record-a",
        sessionId: "session-a"
      },
      { type: "stopped" }
    );
    expect(stopped).toMatchObject({ status: "stopped", recordKey: "record-a", sessionId: null });
  });
});

describe("parsePlaybackChannelId", () => {
  it("正常通道主键按数字返回", () => {
    expect(parsePlaybackChannelId("31")).toBe(31);
    expect(parsePlaybackChannelId(31)).toBe(31);
  });

  it("20 位国标编码不会被 Number() 抹掉末位，并明确判为不可用", () => {
    // 关键回归：Number("34020000001320000037") === 34020000001320000000，
    // 末两位被抹平，页面会报「通道不存在」而编码其实是对的。
    expect(parsePlaybackChannelId("34020000001320000037")).toBe(0);
  });

  it("空值/非数字/非正数不再回落成默认通道", () => {
    ["", "  ", "abc", "31a", "0", "-5", undefined, null].forEach(value => {
      expect(parsePlaybackChannelId(value)).toBe(0);
    });
  });
});
