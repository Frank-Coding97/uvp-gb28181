import { afterEach, describe, expect, it, vi } from "vitest";
import {
  channelReleaseWatchBudget,
  channelReleaseWatchDelays,
  isChannelBusy,
  isSessionReleased,
  isTeardownStall,
  playbackFailureCode,
  playbackFailureStage,
  waitForChannelRelease,
  waitForNextProbe
} from "./playbackTeardown";

// 后端 app.Response.Fail 的形状:业务错误码在 data.data 里,人话文案在 data.message。
function backendFailure(status: number, errorCode: string, errorStage: string, message: string) {
  return { message, response: { status, data: { message, data: { errorCode, errorStage } } } };
}

describe("record playback teardown failure triage", () => {
  it("classifies a teardown timeout as a stall rather than a media failure", () => {
    const error = backendFailure(504, "playback_unavailable", "teardown", "停止回放会话超时：设备未确认拆除");
    expect(isTeardownStall(error)).toBe(true);
    expect(playbackFailureStage(error)).toBe("teardown");
    expect(playbackFailureCode(error)).toBe("playback_unavailable");
    // 网关类状态码一律按「平台侧还没收敛」处理,不依赖 errorStage 是否规范。
    expect(isTeardownStall(backendFailure(502, "playback_unavailable", "", "无可用媒体节点"))).toBe(true);
    expect(isTeardownStall(new Error("stop failed"))).toBe(false);
  });

  it("treats a vanished session as a released channel and a 429 as a busy channel", () => {
    expect(isSessionReleased(backendFailure(404, "playback_not_found", "not_found", "回放会话不存在"))).toBe(true);
    expect(isSessionReleased(backendFailure(422, "playback_invalid_argument", "control", "回放控制参数或状态不合法"))).toBe(
      false
    );
    expect(isChannelBusy(backendFailure(429, "playback_busy", "busy", "当前通道已有回放会话"))).toBe(true);
    expect(isChannelBusy(backendFailure(504, "playback_unavailable", "teardown", "停止回放会话超时"))).toBe(false);
  });

  it("reads a failure code attached directly to the error object", () => {
    expect(playbackFailureCode({ errorCode: "playback_busy" })).toBe("playback_busy");
    expect(isChannelBusy({ errorCode: "playback_busy" })).toBe(true);
  });

  it("backs off across the release watch instead of probing in a tight loop", () => {
    expect(channelReleaseWatchBudget).toBe(channelReleaseWatchDelays.length);
    for (let index = 1; index < channelReleaseWatchDelays.length; index += 1) {
      expect(channelReleaseWatchDelays[index]).toBeGreaterThanOrEqual(channelReleaseWatchDelays[index - 1]);
    }
  });

  it("stops probing as soon as the channel is released", async () => {
    vi.useFakeTimers();
    const probe = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    const watched = waitForChannelRelease(probe);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(await watched).toBe(true);
    expect(probe).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("gives up after the whole budget without claiming the channel was released", async () => {
    vi.useFakeTimers();
    const probe = vi.fn().mockResolvedValue(false);
    const watched = waitForChannelRelease(probe);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await watched).toBe(false);
    expect(probe).toHaveBeenCalledTimes(channelReleaseWatchBudget);
    vi.useRealTimers();
  });

  it("clamps the probe delay so a runaway loop cannot push the wait out", async () => {
    vi.useFakeTimers();
    const waited = waitForNextProbe(999);
    await vi.advanceTimersByTimeAsync(channelReleaseWatchDelays[channelReleaseWatchDelays.length - 1]);
    await expect(waited).resolves.toBeUndefined();
    vi.useRealTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });
});
