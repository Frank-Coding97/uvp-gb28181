import { describe, expect, it } from "vitest";
import {
  DICT_CODE_PLAYABLE_STATE,
  PLAYABLE_STATE_LABEL_FALLBACK,
  PLAYABLE_STATE_UNKNOWN_TEXT,
  playableStateLabel
} from "./playbackAvailability";

describe("playableStateLabel", () => {
  it("按后端值域给出兜底文案", () => {
    expect(playableStateLabel("available")).toBe("可播放");
    expect(playableStateLabel("offline")).toBe("离线");
    expect(playableStateLabel("missing")).toBe("通道不存在");
    expect(playableStateLabel("forbidden")).toBe("无权访问");
  });

  it("未命中 / 空值统一回落「不可用」", () => {
    expect(playableStateLabel("weird")).toBe(PLAYABLE_STATE_UNKNOWN_TEXT);
    expect(playableStateLabel("")).toBe(PLAYABLE_STATE_UNKNOWN_TEXT);
    expect(playableStateLabel(null)).toBe(PLAYABLE_STATE_UNKNOWN_TEXT);
    expect(playableStateLabel(undefined)).toBe(PLAYABLE_STATE_UNKNOWN_TEXT);
  });

  it("去掉首尾空白再查表", () => {
    expect(playableStateLabel("  offline  ")).toBe("离线");
  });

  it("字典可改已知档位的名字，但改不掉未命中兜底", () => {
    const labels = { ...PLAYABLE_STATE_LABEL_FALLBACK, offline: "设备离线" };
    expect(playableStateLabel("offline", labels)).toBe("设备离线");
    expect(playableStateLabel("available", labels)).toBe("可播放");
    expect(playableStateLabel("weird", labels)).toBe(PLAYABLE_STATE_UNKNOWN_TEXT);
  });

  it("值域锁定：兜底表恰好这 4 档（加档等于改协议，必须显式改这里）", () => {
    expect(Object.keys(PLAYABLE_STATE_LABEL_FALLBACK).sort()).toEqual(["available", "forbidden", "missing", "offline"]);
  });

  it("不修改传入的表（调用方可能直接传字典对象）", () => {
    const labels = { ...PLAYABLE_STATE_LABEL_FALLBACK };
    playableStateLabel("offline", labels);
    expect(labels).toEqual(PLAYABLE_STATE_LABEL_FALLBACK);
  });

  it("字典 code 固定", () => {
    expect(DICT_CODE_PLAYABLE_STATE).toBe("playable_state");
  });
});
