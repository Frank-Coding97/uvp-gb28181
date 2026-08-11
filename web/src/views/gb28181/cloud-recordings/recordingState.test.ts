import { describe, expect, it, vi } from "vitest";

import {
  availabilityPresentation,
  createLatestRequestCoordinator,
  createPlaybackRecovery,
  createPollingController,
  defaultRecordingQuery,
  recordingErrorPresentation
} from "./recordingState";

describe("cloud recording query state", () => {
  it("defaults to the latest 24 hours and page one", () => {
    const now = new Date("2026-08-10T20:00:00.000Z");
    expect(defaultRecordingQuery(now)).toEqual({
      page: 1,
      pageSize: 20,
      start: "2026-08-09T20:00:00.000Z",
      end: "2026-08-10T20:00:00.000Z"
    });
  });

  it("aborts the previous request and only accepts the latest token", () => {
    const coordinator = createLatestRequestCoordinator();
    const first = coordinator.next();
    const second = coordinator.next();
    expect(first.signal.aborted).toBe(true);
    expect(coordinator.isCurrent(first.token)).toBe(false);
    expect(coordinator.isCurrent(second.token)).toBe(true);
    coordinator.dispose();
    expect(second.signal.aborted).toBe(true);
  });
});

describe("cloud recording product states", () => {
  it("maps availability to conclusions and hides unsupported actions", () => {
    expect(availabilityPresentation("available")).toMatchObject({ label: "可播放", canAccess: true });
    expect(availabilityPresentation("node_offline")).toMatchObject({ label: "节点离线", canAccess: false });
    expect(availabilityPresentation("node_missing")).toMatchObject({ label: "节点已移除", canAccess: false });
    expect(availabilityPresentation("file_missing")).toMatchObject({ label: "文件已缺失", canAccess: false });
    expect(availabilityPresentation("access_unavailable")).toMatchObject({ label: "暂不可访问", canAccess: false });
  });

  it("maps HTTP status without exposing backend error text", () => {
    expect(recordingErrorPresentation({ response: { status: 410, data: { message: "/private/path" } } })).toBe("访问凭据已过期");
    expect(recordingErrorPresentation({ response: { status: 403 } })).toBe("访问权限已失效");
    expect(recordingErrorPresentation({ response: { status: 503 } })).toBe("录像节点暂不可用");
    expect(recordingErrorPresentation(new Error("secret internal detail"))).toBe("请求失败，请稍后重试");
  });
});

describe("cloud recording playback and polling", () => {
  it("allows one capability recovery and preserves currentTime", () => {
    const recovery = createPlaybackRecovery();
    expect(recovery.next(37.5)).toEqual({ retry: true, resumeAt: 37.5 });
    expect(recovery.next(51)).toEqual({ retry: false, resumeAt: 51 });
    recovery.reset();
    expect(recovery.next(8)).toEqual({ retry: true, resumeAt: 8 });
  });

  it("does not publish a pending poll after stop", async () => {
    vi.useFakeTimers();
    let resolve!: (value: number) => void;
    const values: number[] = [];
    const poller = createPollingController(() => new Promise<number>(done => (resolve = done)), value => values.push(value), 1000);
    poller.start();
    poller.stop();
    resolve(7);
    await Promise.resolve();
    expect(values).toEqual([]);
    vi.useRealTimers();
  });
});
