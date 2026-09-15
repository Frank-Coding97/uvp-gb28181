import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  batchDeleteRecordingFiles,
  contentURL,
  cancelRecordingDownload,
  createRecordingDownload,
  deleteRecordingFile,
  getRecordingDownload,
  getRecordingDetail,
  issueRecordingAccess,
  listActiveRecordings,
  listRecordingFiles,
  listRecordingOptions,
  listReconciliations,
  stopActiveRecording,
  triggerReconciliation
} from "./api";

describe("cloud recording API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("sends supplied filters and AbortSignal without empty values", async () => {
    const controller = new AbortController();
    await listRecordingFiles(
      { page: 2, pageSize: 30, start: "2026-08-09T00:00:00Z", end: "2026-08-10T00:00:00Z", keyword: "camera", deviceId: "" },
      controller.signal
    );
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/cloud-recordings/files", {
      params: { page: 2, pageSize: 30, start: "2026-08-09T00:00:00Z", end: "2026-08-10T00:00:00Z", keyword: "camera" },
      signal: controller.signal
    });
  });

  it("keeps opaque string IDs across detail and playback access", async () => {
    const id = "9007199254740993";
    await getRecordingDetail(id);
    await issueRecordingAccess(id, "play");
    expect(request).toHaveBeenNthCalledWith(1, "get", `/api/gb28181/cloud-recordings/files/${id}`, undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(2, "post", `/api/gb28181/cloud-recordings/files/${id}/access`, { data: { mode: "play" } }, { showErrorMessage: false });
  });

  it("creates, polls and cancels opaque download tasks without exposing a ticket", async () => {
    const fileId = "9007199254740993";
    const taskId = "opaque-task-id";
    await createRecordingDownload(fileId);
    await getRecordingDownload(taskId);
    await cancelRecordingDownload(taskId);
    expect(request).toHaveBeenNthCalledWith(1, "post", `/api/gb28181/cloud-recordings/files/${fileId}/downloads`, undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(2, "get", `/api/gb28181/cloud-recordings/downloads/${taskId}`, undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(3, "delete", `/api/gb28181/cloud-recordings/downloads/${taskId}`, undefined, { showErrorMessage: false });
  });

  it("deletes one recording or a batch by opaque IDs", async () => {
    await deleteRecordingFile("9007199254740993");
    await batchDeleteRecordingFiles(["9007199254740993", "9007199254740994"]);
    expect(request).toHaveBeenNthCalledWith(1, "delete", "/api/gb28181/cloud-recordings/files/9007199254740993", undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(2, "post", "/api/gb28181/cloud-recordings/files/batch-delete", { data: { ids: ["9007199254740993", "9007199254740994"] } }, { showErrorMessage: false });
  });

  it("covers options, active and reconciliation control APIs", async () => {
    await listRecordingOptions({ start: "a", end: "b" });
    await listActiveRecordings();
    await stopActiveRecording("77");
    await listReconciliations();
    await triggerReconciliation({ nodeIds: [11, 12] });
    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/cloud-recordings/files/options", { params: { start: "a", end: "b" } });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/cloud-recordings/active", undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(3, "post", "/api/gb28181/cloud-recordings/active/77/stop", undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(4, "get", "/api/gb28181/cloud-recordings/reconciliations", undefined, { showErrorMessage: false });
    expect(request).toHaveBeenNthCalledWith(5, "post", "/api/gb28181/cloud-recordings/reconciliations", { data: { nodeIds: [11, 12] } });
  });

  it("builds a same-origin streaming URL without fetching a Blob", () => {
    expect(contentURL("41", "signed+/=")).toBe("/api/gb28181/cloud-recordings/content/41?cap=signed%2B%2F%3D");
  });
});
