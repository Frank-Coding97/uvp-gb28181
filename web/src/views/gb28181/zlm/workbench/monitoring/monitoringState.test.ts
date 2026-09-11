import { describe, expect, it } from "vitest";

import {
  buildStreamRequestQuery,
  createNetworkFilters,
  createStreamFilters,
  createViewerFilters,
  overviewAsRuntime,
  orderClusterNodesByRisk,
  sameNodeTargets,
  scopeRange
} from "./monitoringState";

describe("monitoring panel state", () => {
  it("initializes deep-link filters without owning the router", () => {
    expect(createStreamFilters({ app: " live ", recordingHls: "true" })).toEqual({
      schema: "",
      vhost: "",
      app: " live ",
      stream: "",
      recording: "hls"
    });
    expect(createViewerFilters({ schema: "rtsp", stream: "camera" })).toMatchObject({ schema: "rtsp", stream: "camera" });
    expect(createNetworkFilters({ peerIp: "192.0.2.1", localPort: 18080 })).toMatchObject({ peerIp: "192.0.2.1", localPort: "18080", page: 1, pageSize: 20 });
  });

  it("builds backend stream filters while keeping pagination and recording semantics", () => {
    expect(buildStreamRequestQuery(createStreamFilters({ schema: "rtsp", recording: "mp4" }), 2, 50, 7)).toEqual({
      nodeId: 7,
      page: 2,
      pageSize: 50,
      schema: "rtsp",
      vhost: undefined,
      app: undefined,
      stream: undefined,
      recordingMp4: true,
      recordingHls: undefined
    });
  });

  it("uses an explicit range key and rejects cross-node batch actions", () => {
    expect(scopeRange("all", null)).toBe("all");
    expect(scopeRange(7, 7)).toBe("node:7");
    expect(sameNodeTargets([])).toBe(false);
    expect(sameNodeTargets([
      { nodeId: 1, media: { schema: "rtsp", vhost: "v", app: "a", stream: "one" } },
      { nodeId: 2, media: { schema: "rtsp", vhost: "v", app: "a", stream: "two" } }
    ])).toBe(false);
  });

  it("keeps aggregate partial results auditable and unknown when no node was sampled", () => {
    const runtime = overviewAsRuntime({
      nodes: [],
      streams: [],
      metrics: {
        sampledNodeCount: 2,
        mediaSourceCount: 0,
        multiMediaSourceMuxerCount: 0,
        tcpServerCount: 0,
        tcpSessionCount: 0,
        udpServerCount: 0,
        udpSessionCount: 0,
        tcpClientCount: 0,
        socketCount: 0,
        networkSessionCount: 0,
        netThreadLoadAvg: 0,
        workThreadLoadAvg: 0,
        streamCount: 0
      },
      partial: true,
      asOf: "2026-08-30T00:00:00Z",
      metricsSampledNodeIds: [],
      mediaSampledNodeIds: [],
      successfulNodeIds: [],
      failedNodeIds: [],
      errors: []
    });
    expect(runtime.status).toBe("partial");
    expect(runtime.metricsComplete).toBe(false);
    expect(runtime.mediaFreshness).toBe("unavailable");
    expect(runtime.streams).toBeUndefined();
  });

  it("orders unhealthy cluster nodes before healthy nodes", () => {
    const nodes = [
      { nodeId: 1, name: "healthy", state: "active", status: "fresh" },
      { nodeId: 2, name: "offline", state: "offline", status: "unavailable" },
      { nodeId: 3, name: "partial", state: "active", status: "partial" }
    ] as unknown as Parameters<typeof orderClusterNodesByRisk>[0];

    expect(orderClusterNodesByRisk(nodes).map(node => node.nodeId)).toEqual([2, 3, 1]);
    expect(nodes.map(node => node.nodeId)).toEqual([1, 2, 3]);
  });
});
