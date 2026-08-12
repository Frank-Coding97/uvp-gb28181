import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { useRecordingDownloadStore } from "./recording-downloads";

describe("recording download store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("keeps download tasks in memory and derives active count", () => {
    const store = useRecordingDownloadStore();
    store.upsert({ taskId: "one", fileId: "file-1", fileName: "a.mp4", status: "streaming", bytesSent: 2, createdAt: "now", expiresAt: "later" });
    store.upsert({ taskId: "two", fileId: "file-2", fileName: "b.mp4", status: "completed", bytesSent: 4, createdAt: "now", expiresAt: "later" });
    expect(store.activeCount).toBe(1);
    expect(JSON.stringify(store.$state)).not.toContain("contentUrl");
  });

  it("removes terminal tasks through the coordinator snapshot reconciliation", () => {
    const store = useRecordingDownloadStore();
    store.upsert({ taskId: "one", fileId: "file-1", fileName: "a.mp4", status: "completed", bytesSent: 2, createdAt: "now", expiresAt: "later" });
    store.remove("one");
    expect(store.tasks).toEqual([]);
  });
});
