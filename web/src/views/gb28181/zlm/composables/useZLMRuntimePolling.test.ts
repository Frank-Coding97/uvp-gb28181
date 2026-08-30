import { afterEach, describe, expect, it, vi } from "vitest";

import { createZLMRuntimePollingController } from "./useZLMRuntimePolling";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (cause: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

describe("ZLM runtime polling", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("aborts the old node and ignores its late response", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const signals: AbortSignal[] = [];
    const publish = vi.fn();
    const load = vi.fn((nodeId: number, signal: AbortSignal) => {
      signals.push(signal);
      return nodeId === 1 ? first.promise : second.promise;
    });
    const polling = createZLMRuntimePollingController({ load, publish, intervalMs: 10_000 });

    polling.setNode(1);
    polling.start();
    polling.setNode(2);
    expect(signals[0].aborted).toBe(true);

    second.resolve("new");
    await Promise.resolve();
    first.resolve("stale");
    await Promise.resolve();

    expect(publish).toHaveBeenCalledTimes(1);
    expect(publish).toHaveBeenCalledWith("new", 2);
    polling.dispose();
  });

  it("pauses while hidden or editing and resumes with an immediate refresh", async () => {
    vi.useFakeTimers();
    const load = vi.fn(async (nodeId: number) => nodeId);
    const publish = vi.fn();
    const polling = createZLMRuntimePollingController({ load, publish, intervalMs: 1000 });
    polling.setNode(7);
    polling.start();
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(1);

    polling.setVisible(false);
    await vi.advanceTimersByTimeAsync(5000);
    expect(load).toHaveBeenCalledTimes(1);

    polling.setVisible(true);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(2);

    polling.setPaused(true);
    await vi.advanceTimersByTimeAsync(5000);
    expect(load).toHaveBeenCalledTimes(2);

    polling.setPaused(false);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(3);
    polling.dispose();
  });

  it("pauses for inactive, editing and danger states independently", async () => {
    const requests: AbortSignal[] = [];
    const load = vi.fn((_: number, signal: AbortSignal) => {
      requests.push(signal);
      return Promise.resolve("ok");
    });
    const polling = createZLMRuntimePollingController({ load, publish: vi.fn(), intervalMs: 10_000 });

    polling.setNode(7);
    polling.start();
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(1);

    polling.setActive(false);
    expect(requests[0].aborted).toBe(true);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(1);

    polling.setActive(true);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(2);
    polling.setEditing(true);
    expect(requests[1].aborted).toBe(true);
    polling.setEditing(false);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(3);

    polling.setDanger(true);
    expect(requests[2].aborted).toBe(true);
    polling.setDanger(false);
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(4);
    polling.dispose();
  });

  it("leaves no timer or publish path after dispose", async () => {
    vi.useFakeTimers();
    const pending = deferred<number>();
    const publish = vi.fn();
    const polling = createZLMRuntimePollingController({ load: () => pending.promise, publish, intervalMs: 1000 });
    polling.setNode(4);
    polling.start();
    polling.dispose();
    pending.resolve(4);
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(5000);
    expect(publish).not.toHaveBeenCalled();
  });

  it("survives 100 slow node switches without stale publish or timer leaks", async () => {
    vi.useFakeTimers();
    const requests: Array<{
      nodeId: number;
      signal: AbortSignal;
      task: ReturnType<typeof deferred<number>>;
    }> = [];
    const publish = vi.fn();
    const load = vi.fn((nodeId: number, signal: AbortSignal) => {
      const task = deferred<number>();
      requests.push({ nodeId, signal, task });
      return task.promise;
    });
    const polling = createZLMRuntimePollingController({ load, publish, intervalMs: 1000 });

    polling.setNode(1);
    polling.start();
    for (let nodeId = 2; nodeId <= 100; nodeId += 1) polling.setNode(nodeId);

    expect(requests).toHaveLength(100);
    expect(requests.slice(0, -1).every(request => request.signal.aborted)).toBe(true);
    expect(requests.at(-1)?.signal.aborted).toBe(false);

    requests.at(-1)?.task.resolve(100);
    await Promise.resolve();
    await Promise.resolve();
    for (const request of requests.slice(0, -1).reverse()) request.task.resolve(request.nodeId);
    await Promise.resolve();
    await Promise.resolve();

    expect(publish).toHaveBeenCalledTimes(1);
    expect(publish).toHaveBeenCalledWith(100, 100);
    expect(vi.getTimerCount()).toBe(1);

    polling.dispose();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("reports a synchronous loader failure and keeps the polling chain alive", async () => {
    vi.useFakeTimers();
    const failure = new Error("synchronous load failure");
    const load = vi.fn()
      .mockImplementationOnce(() => { throw failure; })
      .mockResolvedValue(7);
    const publish = vi.fn();
    const onError = vi.fn();
    const polling = createZLMRuntimePollingController({ load, publish, onError, intervalMs: 1000 });
    polling.setNode(7);

    expect(() => polling.start()).not.toThrow();
    await Promise.resolve();
    await Promise.resolve();
    expect(onError).toHaveBeenCalledWith(failure, 7);

    await vi.advanceTimersByTimeAsync(1000);
    expect(load).toHaveBeenCalledTimes(2);
    expect(publish).toHaveBeenCalledWith(7, 7);
    polling.dispose();
  });
});
