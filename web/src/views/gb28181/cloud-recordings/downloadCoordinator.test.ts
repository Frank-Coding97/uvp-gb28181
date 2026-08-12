import { describe, expect, it, vi } from "vitest";
import { createDownloadCoordinator } from "./downloadCoordinator";

const task = (taskId: string, fileId: string, status = "ready") => ({
  taskId, fileId, status, bytesSent: 0, createdAt: "2026-08-12T00:00:00Z", expiresAt: "2026-08-12T00:10:00Z"
});

describe("download coordinator", () => {
  it("deduplicates a file, limits browser starts to two, and starts only the returned same-origin URL", async () => {
    const create = vi.fn()
      .mockResolvedValueOnce({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" })
      .mockResolvedValueOnce({ task: task("two", "file-2"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" })
      .mockResolvedValueOnce({ task: task("three", "file-3"), contentUrl: "/api/gb28181/cloud-recordings/downloads/three/content" });
    const startNativeDownload = vi.fn();
    const coordinator = createDownloadCoordinator({ create, get: vi.fn(), cancel: vi.fn(), startNativeDownload, pollIntervalMs: 99999 });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.enqueue({ fileId: "file-2", fileName: "two.mp4" });
    await coordinator.enqueue({ fileId: "file-3", fileName: "three.mp4" });
    expect(create).toHaveBeenCalledTimes(2);
    expect(startNativeDownload).toHaveBeenCalledWith("/api/gb28181/cloud-recordings/downloads/one/content", "one.mp4");
    expect(startNativeDownload).toHaveBeenCalledWith("/api/gb28181/cloud-recordings/downloads/two/content", "two.mp4");
    expect(coordinator.snapshot().find(item => item.fileId === "file-3")?.status).toBe("queued");
  });

  it("polls progress, releases a terminal slot, and starts the queued task", async () => {
    const get = vi.fn().mockResolvedValue({ task: task("one", "file-1", "completed") });
    const create = vi.fn()
      .mockResolvedValueOnce({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" })
      .mockResolvedValueOnce({ task: task("two", "file-2"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" })
      .mockResolvedValueOnce({ task: task("three", "file-3"), contentUrl: "/api/gb28181/cloud-recordings/downloads/three/content" });
    const startNativeDownload = vi.fn();
    const coordinator = createDownloadCoordinator({ create, get, cancel: vi.fn(), startNativeDownload, pollIntervalMs: 99999 });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.enqueue({ fileId: "file-2", fileName: "two.mp4" });
    await coordinator.enqueue({ fileId: "file-3", fileName: "three.mp4" });
    await coordinator.refresh("one");
    expect(create).toHaveBeenCalledTimes(3);
    expect(startNativeDownload).toHaveBeenLastCalledWith("/api/gb28181/cloud-recordings/downloads/three/content", "three.mp4");
  });

  it("keeps an active slot after a transient status failure", async () => {
    const get = vi.fn().mockRejectedValueOnce(new Error("temporary network failure"));
    const create = vi.fn()
      .mockResolvedValueOnce({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" })
      .mockResolvedValueOnce({ task: task("two", "file-2"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" })
      .mockResolvedValueOnce({ task: task("three", "file-3"), contentUrl: "/api/gb28181/cloud-recordings/downloads/three/content" });
    const coordinator = createDownloadCoordinator({ create, get, cancel: vi.fn(), startNativeDownload: vi.fn(), pollIntervalMs: 99999 });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.enqueue({ fileId: "file-2", fileName: "two.mp4" });
    await coordinator.enqueue({ fileId: "file-3", fileName: "three.mp4" });
    await coordinator.refresh("one");
    expect(create).toHaveBeenCalledTimes(2);
    expect(coordinator.snapshot().find(item => item.taskId === "one")?.status).toBe("ready");
    expect(coordinator.snapshot().find(item => item.taskId === "one")?.errorCode).toBe("status_unavailable");
    expect(coordinator.snapshot().find(item => item.fileId === "file-3")?.status).toBe("queued");
  });

  it("cancels server tasks and retries from zero with a new task", async () => {
    const cancel = vi.fn().mockResolvedValue({ task: task("one", "file-1", "cancelled") });
    const create = vi.fn()
      .mockResolvedValueOnce({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" })
      .mockResolvedValueOnce({ task: task("two", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" });
    const coordinator = createDownloadCoordinator({ create, get: vi.fn(), cancel, startNativeDownload: vi.fn(), pollIntervalMs: 99999 });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.cancel("one");
    await coordinator.retry("one");
    expect(cancel).toHaveBeenCalledWith("one");
    expect(create).toHaveBeenCalledTimes(2);
    expect(coordinator.snapshot().some(item => item.taskId === "two" && item.bytesSent === 0)).toBe(true);
  });

  it("rejects a cross-origin content URL before starting the browser download", async () => {
    const startNativeDownload = vi.fn();
    const coordinator = createDownloadCoordinator({
      create: vi.fn().mockResolvedValue({ task: task("one", "file-1"), contentUrl: "https://untrusted.example/download" }),
      get: vi.fn(), cancel: vi.fn(), startNativeDownload, pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    expect(startNativeDownload).not.toHaveBeenCalled();
    expect(coordinator.snapshot()[0]?.status).toBe("failed");
    expect(coordinator.snapshot()[0]?.errorCode).toBe("content_url_invalid");
  });

  it("rejects a content URL whose task id differs from the response task", async () => {
    const startNativeDownload = vi.fn();
    const coordinator = createDownloadCoordinator({
      create: vi.fn().mockResolvedValue({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" }),
      get: vi.fn(), cancel: vi.fn(), startNativeDownload, pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    expect(startNativeDownload).not.toHaveBeenCalled();
    expect(coordinator.snapshot()[0]?.errorCode).toBe("content_url_invalid");
  });

  it("rejects query and fragment variants of the content URL", async () => {
    const startNativeDownload = vi.fn();
    const coordinator = createDownloadCoordinator({
      create: vi.fn().mockResolvedValue({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content?redirect=https://untrusted.example#fragment" }),
      get: vi.fn(), cancel: vi.fn(), startNativeDownload, pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    expect(startNativeDownload).not.toHaveBeenCalled();
    expect(coordinator.snapshot()[0]?.errorCode).toBe("content_url_invalid");
  });

  it("treats an unparsable content URL as invalid", async () => {
    const coordinator = createDownloadCoordinator({
      create: vi.fn().mockResolvedValue({ task: task("one", "file-1"), contentUrl: "http://[" }),
      get: vi.fn(), cancel: vi.fn(), startNativeDownload: vi.fn(), pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    expect(coordinator.snapshot()[0]?.errorCode).toBe("content_url_invalid");
  });

  it("clears terminal items without allowing later publishes to restore them", async () => {
    const coordinator = createDownloadCoordinator({
      create: vi.fn().mockResolvedValue({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" }),
      get: vi.fn().mockResolvedValue({ task: task("one", "file-1", "completed") }), cancel: vi.fn(), startNativeDownload: vi.fn(), pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.refresh("one");
    coordinator.clearTerminal();
    await coordinator.refreshAll();
    expect(coordinator.snapshot()).toEqual([]);
  });

  it("cancels every active task while containing individual cancellation failures", async () => {
    const cancel = vi.fn().mockRejectedValueOnce(new Error("cancel unavailable")).mockResolvedValueOnce({ task: task("two", "file-2", "cancelled") });
    const coordinator = createDownloadCoordinator({
      create: vi.fn()
        .mockResolvedValueOnce({ task: task("one", "file-1"), contentUrl: "/api/gb28181/cloud-recordings/downloads/one/content" })
        .mockResolvedValueOnce({ task: task("two", "file-2"), contentUrl: "/api/gb28181/cloud-recordings/downloads/two/content" }),
      get: vi.fn(), cancel, startNativeDownload: vi.fn(), pollIntervalMs: 99999
    });
    await coordinator.enqueue({ fileId: "file-1", fileName: "one.mp4" });
    await coordinator.enqueue({ fileId: "file-2", fileName: "two.mp4" });
    await expect(coordinator.cancelAll()).resolves.toBeUndefined();
    expect(cancel).toHaveBeenCalledTimes(2);
  });
});
