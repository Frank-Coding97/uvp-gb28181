import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  closeZLMStream,
  fetchZLMStreamSnapshot,
  getZLMNodeRuntime,
  getZLMOverview,
  getZLMRecordingStatus,
  kickZLMSession,
  listZLMNetworkSessions,
  listZLMStreams,
  preflightCloseZLMStream,
  preflightForceStopZLMRecording,
  preflightStartZLMRecording,
  preflightStopZLMRecording,
  snapshotZLMStreamURL,
  startZLMRecording,
  stopZLMRecording,
  forceStopZLMRecording,
  type ZLMMediaIdentity
} from "./gb28181-zlm-runtime";

const media: ZLMMediaIdentity = { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "34020000001320000001" };

describe("ZLM runtime API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("uses only same-origin typed overview and node runtime routes", async () => {
    const controller = new AbortController();
    await getZLMOverview(controller.signal);
    await getZLMNodeRuntime(7, controller.signal);

    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/zlm/overview", { signal: controller.signal });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/zlm/nodes/7/runtime", { signal: controller.signal });
  });

  it("keeps filters in query params and drops empty optional values", async () => {
    const controller = new AbortController();
    await listZLMStreams({ nodeId: 7, page: 2, pageSize: 30, app: "live", stream: "" }, controller.signal);
    await listZLMNetworkSessions(7, { page: 3, pageSize: 20, peerIp: "", localPort: 8000 }, controller.signal);

    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/zlm/streams", {
      params: { nodeId: 7, page: 2, pageSize: 30, app: "live" },
      signal: controller.signal
    });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/zlm/nodes/7/sessions/network", {
      params: { page: 3, pageSize: 20, localPort: 8000 },
      signal: controller.signal
    });
  });

  it("uses immutable preflight fingerprints for destructive stream actions", async () => {
    await preflightCloseZLMStream(7, media);
    await closeZLMStream(7, media, "sha256:fingerprint");
    await kickZLMSession(7, media, "opaque-session-id");

    expect(request).toHaveBeenNthCalledWith(1, "post", "/api/gb28181/zlm/nodes/7/streams/close/preflight", {
      data: { nodeId: 7, media }
    });
    expect(request).toHaveBeenNthCalledWith(2, "post", "/api/gb28181/zlm/nodes/7/streams/close", {
      data: { target: { nodeId: 7, media }, fingerprint: "sha256:fingerprint" }
    });
    expect(request).toHaveBeenNthCalledWith(3, "post", "/api/gb28181/zlm/nodes/7/sessions/kick", {
      data: { nodeId: 7, media, identifier: "opaque-session-id" }
    });
  });

  it("builds snapshot and recording status requests without putting media identity in a path", async () => {
    expect(snapshotZLMStreamURL(7, media)).toBe(
      "/api/gb28181/zlm/nodes/7/streams/snapshot?schema=rtsp&vhost=__defaultVhost__&app=live&stream=34020000001320000001"
    );
    const controller = new AbortController();
    await fetchZLMStreamSnapshot(7, media, controller.signal);
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/zlm/nodes/7/streams/snapshot", {
      params: media,
      responseType: "blob",
      signal: controller.signal
    });
    await getZLMRecordingStatus(7, media, 1);
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/zlm/nodes/7/recordings/runtime/status", {
      params: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "34020000001320000001", type: 1 }
    });
  });

  it("keeps ordinary and force recording controls on separate typed routes", async () => {
    const base = { media, type: 1 as const };
    await preflightStartZLMRecording(7, base);
    await startZLMRecording(7, base);
    await preflightStopZLMRecording(7, base);
    await stopZLMRecording(7, { ...base, fingerprint: "stop-fp" });
    await preflightForceStopZLMRecording(7, { ...base, reason: "incident response" });
    await forceStopZLMRecording(7, { ...base, fingerprint: "force-fp", reason: "incident response" });

    expect(request).toHaveBeenNthCalledWith(1, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/start/preflight", {
      data: { target: { nodeId: 7, media }, type: 1 }
    });
    expect(request).toHaveBeenNthCalledWith(2, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/start", {
      data: { target: { nodeId: 7, media }, type: 1 }
    });
    expect(request).toHaveBeenNthCalledWith(3, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/stop/preflight", {
      data: { target: { nodeId: 7, media }, type: 1 }
    });
    expect(request).toHaveBeenNthCalledWith(4, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/stop", {
      data: { target: { nodeId: 7, media }, type: 1, fingerprint: "stop-fp" }
    });
    expect(request).toHaveBeenNthCalledWith(5, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/force-stop/preflight", {
      data: { target: { nodeId: 7, media }, type: 1, reason: "incident response" }
    });
    expect(request).toHaveBeenNthCalledWith(6, "post", "/api/gb28181/zlm/nodes/7/recordings/runtime/force-stop", {
      data: { target: { nodeId: 7, media }, type: 1, fingerprint: "force-fp", reason: "incident response" }
    });
  });

  it("contains no browser-side ZLM management endpoint or secret parameter", () => {
    const source = readFileSync(resolve(process.cwd(), "src/api/gb28181-zlm-runtime.ts"), "utf8");
    expect(source).not.toMatch(/\/index\/api\//i);
    expect(source).not.toMatch(/apiSecret|[?&]secret=/i);
  });
});
