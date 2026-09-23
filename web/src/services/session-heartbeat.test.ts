import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const heartbeat = vi.hoisted(() => vi.fn());
const hasRefreshToken = vi.hoisted(() => vi.fn());

vi.mock("@/api/online-user", () => ({ sessionHeartbeatAPI: heartbeat }));
vi.mock("@/utils/auth", () => ({ hasRefreshToken }));

import { startSessionHeartbeat, stopSessionHeartbeat } from "./session-heartbeat";

const flush = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

describe("session heartbeat coordinator", () => {
  let visibility: DocumentVisibilityState;

  beforeEach(() => {
    vi.useFakeTimers();
    visibility = "visible";
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility);
    heartbeat.mockResolvedValue({ code: 0, data: { online: true }, message: "" });
    hasRefreshToken.mockReturnValue(true);
    stopSessionHeartbeat();
  });

  afterEach(() => {
    stopSessionHeartbeat();
    vi.useRealTimers();
  });

  it("keeps one timer across duplicate starts and sends at most once per minute", async () => {
    startSessionHeartbeat();
    startSessionHeartbeat();
    await flush();

    expect(heartbeat).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(1);
    startSessionHeartbeat();
    await flush();
    expect(heartbeat).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(1);

    await vi.advanceTimersByTimeAsync(59_999);
    expect(heartbeat).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(heartbeat).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(1);
  });

  it("pauses while hidden and heartbeats immediately when visible again", async () => {
    visibility = "hidden";
    startSessionHeartbeat();
    await flush();
    expect(heartbeat).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);

    visibility = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
    await flush();
    expect(heartbeat).toHaveBeenCalledTimes(1);

    visibility = "hidden";
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(120_000);
    expect(heartbeat).toHaveBeenCalledTimes(1);
  });

  it("does not overlap an in-flight heartbeat", async () => {
    let resolveHeartbeat!: () => void;
    heartbeat.mockReturnValueOnce(new Promise<void>(resolve => { resolveHeartbeat = resolve; }));
    startSessionHeartbeat();
    await flush();

    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(120_000);
    expect(heartbeat).toHaveBeenCalledTimes(1);

    resolveHeartbeat();
    await flush();
    expect(vi.getTimerCount()).toBe(1);
  });

  it("retries after a transient 503 without clearing login state", async () => {
    heartbeat.mockRejectedValueOnce({ response: { status: 503 } });
    startSessionHeartbeat();
    await flush();
    expect(hasRefreshToken()).toBe(true);

    await vi.advanceTimersByTimeAsync(60_000);
    expect(heartbeat).toHaveBeenCalledTimes(2);
  });

  it("stops all future work on logout", async () => {
    startSessionHeartbeat();
    await flush();
    stopSessionHeartbeat();
    await vi.advanceTimersByTimeAsync(180_000);

    expect(heartbeat).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("activates immediately when login occurs after app startup", async () => {
    hasRefreshToken.mockReturnValue(false);
    startSessionHeartbeat();
    await flush();
    expect(heartbeat).not.toHaveBeenCalled();

    hasRefreshToken.mockReturnValue(true);
    startSessionHeartbeat();
    await flush();
    expect(heartbeat).toHaveBeenCalledTimes(1);
  });
});
