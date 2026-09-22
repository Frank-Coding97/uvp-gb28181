import { describe, expect, it } from "vitest";
import { FOLLOW_PAUSE_DISTANCE, FOLLOW_RESUME_DISTANCE, resolveFollowAction } from "./follow-scroll";

describe("resolveFollowAction", () => {
  it("resumes following once scrolling reaches the bottom", () => {
    // 本次要修的行为：暂停后再滚回底部，应当自动恢复跟随，不用去点按钮。
    expect(resolveFollowAction(0, false)).toBe("resume");
    expect(resolveFollowAction(FOLLOW_RESUME_DISTANCE, false)).toBe("resume");
    expect(resolveFollowAction(-8, false)).toBe("resume");
  });

  it("pauses following once the reader scrolls away from the bottom", () => {
    expect(resolveFollowAction(FOLLOW_PAUSE_DISTANCE + 1, true)).toBe("pause");
    expect(resolveFollowAction(600, true)).toBe("pause");
  });

  it("keeps the current state inside the hysteresis band", () => {
    // 等阈值会让恰好停在边界的滚动反复切换，所以两阈值之间不改变状态。
    expect(FOLLOW_RESUME_DISTANCE).toBeLessThan(FOLLOW_PAUSE_DISTANCE);
    for (const distance of [FOLLOW_RESUME_DISTANCE + 1, 50, FOLLOW_PAUSE_DISTANCE]) {
      expect(resolveFollowAction(distance, true)).toBe("keep");
      expect(resolveFollowAction(distance, false)).toBe("keep");
    }
  });

  it("is idempotent when the state already matches the position", () => {
    expect(resolveFollowAction(0, true)).toBe("keep");
    expect(resolveFollowAction(2000, false)).toBe("keep");
    expect(resolveFollowAction(120, false)).toBe("keep");
  });
});
