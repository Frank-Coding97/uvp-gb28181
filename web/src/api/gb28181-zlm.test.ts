import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { getZLMNodeRestartStatus, listSchedulerLogs, probeZLMNode, restartZLMNode, updateZLMNode } from "./gb28181-zlm";

describe("ZLM node and scheduler API contract", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("allows candidate host and API port updates through UVP", async () => {
    await updateZLMNode(7, { host: "10.0.0.7", apiPort: 8080 });
    expect(request).toHaveBeenCalledWith("put", "/api/gb28181/zlm/nodes/7", {
      data: { host: "10.0.0.7", apiPort: 8080 }
    });
  });

  it("probes a candidate node without creating it", async () => {
    const candidate = { name: "edge-a", host: "10.0.0.8", apiPort: 18080, apiSecret: "secret" };
    await probeZLMNode(candidate);
    expect(request).toHaveBeenCalledWith("post", "/api/gb28181/zlm/nodes/probe", { data: candidate });
  });

  it("starts and polls an accepted restart operation", async () => {
    const controller = new AbortController();
    await restartZLMNode(7, 500);
    await getZLMNodeRestartStatus(7, "operation-id", controller.signal);

    expect(request).toHaveBeenNthCalledWith(1, "post", "/api/gb28181/zlm/nodes/7/restart", { data: { graceMS: 500 } });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/zlm/nodes/7/restart", {
      params: { operationId: "operation-id" },
      signal: controller.signal
    });
  });

  it("sends typed scheduler filters as query params", async () => {
    await listSchedulerLogs({ nodeId: 7, algorithm: "weighted", result: "success", streamId: "camera", limit: 50 });
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/zlm/scheduler/logs", {
      params: { nodeId: 7, algorithm: "weighted", result: "success", streamId: "camera", limit: 50 }
    });
  });
});
