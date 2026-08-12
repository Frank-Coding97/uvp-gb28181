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
});
