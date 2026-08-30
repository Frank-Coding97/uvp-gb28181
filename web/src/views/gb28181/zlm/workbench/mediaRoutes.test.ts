import { describe, expect, it } from "vitest";

import { LEGACY_MEDIA_ROUTE_PATHS, MEDIA_NODE_DETAIL, MEDIA_WORKSPACES, resolveLegacyMediaRoute } from "./mediaRoutes";

describe("media workbench route contract", () => {
  it("defines six ordered workspaces with stable defaults and unique views", () => {
    expect(MEDIA_WORKSPACES.map(item => [item.path, item.sort])).toEqual([
      ["/media/overview", 10],
      ["/media/monitoring", 20],
      ["/media/ingress", 30],
      ["/media/recordings", 40],
      ["/media/nodes", 50],
      ["/media/scheduling", 60]
    ]);
    expect(new Set(MEDIA_WORKSPACES.map(item => item.path)).size).toBe(6);
    for (const workspace of MEDIA_WORKSPACES) {
      expect(workspace.allowedViews).toContain(workspace.defaultView);
      expect(new Set(workspace.allowedViews).size).toBe(workspace.allowedViews.length);
    }
    expect(MEDIA_NODE_DETAIL).toMatchObject({
      path: "/media/nodes/:id",
      defaultView: "overview",
      allowedViews: ["overview", "runtime", "config"]
    });
  });

  it("covers the 13 visible legacy routes plus the hidden node detail", () => {
    expect(LEGACY_MEDIA_ROUTE_PATHS).toHaveLength(14);
    expect(new Set(LEGACY_MEDIA_ROUTE_PATHS).size).toBe(14);

    const destinations = LEGACY_MEDIA_ROUTE_PATHS.map(path => resolveLegacyMediaRoute(path.replace(":id", "23")));

    expect(destinations.every(location => location?.replace)).toBe(true);
    expect(destinations.every(location => location?.path.startsWith("/media/"))).toBe(true);
  });

  it("maps runtime to node detail only for a valid explicit node", () => {
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime", { nodeId: "2" })).toEqual({
      path: "/media/nodes/2",
      query: { view: "runtime" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime", { nodeId: "0" })).toEqual({
      path: "/media/overview",
      query: { focus: "runtime" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime")).toEqual({
      path: "/media/overview",
      query: { focus: "runtime" },
      replace: true
    });
  });

  it("keeps only route-specific scalar filters and normalizes enum values", () => {
    expect(
      resolveLegacyMediaRoute("/gb28181/zlm/streams", {
        nodeId: 3,
        app: "live",
        stream: "camera-01",
        originType: "rtp_push",
        recording: "true",
        recordingMp4: "false",
        recordingHls: true,
        unknown: "discard-me"
      })
    ).toEqual({
      path: "/media/monitoring",
      query: {
        view: "streams",
        nodeId: "3",
        app: "live",
        stream: "camera-01",
        originType: "rtp_push",
        recording: "true",
        recordingMp4: "false",
        recordingHls: "true"
      },
      replace: true
    });

    expect(
      resolveLegacyMediaRoute("/gb28181/zlm/proxies", {
        view: "invalid",
        kind: "push",
        nodeId: "9"
      })
    ).toEqual({
      path: "/media/ingress",
      query: { view: "push", nodeId: "9" },
      replace: true
    });
  });

  it("drops secrets, source URLs, arrays, invalid identifiers and oversized values", () => {
    const result = resolveLegacyMediaRoute("/gb28181/zlm/rtp-servers", {
      nodeId: "-2",
      app: ["live"],
      stream: "x".repeat(129),
      ssrc: "0100000001",
      apiSecret: "secret",
      token: "token",
      sourceUrl: "rtsp://user:password@example.test/live",
      view: "config"
    });

    expect(result).toEqual({
      path: "/media/ingress",
      query: { view: "rtp", ssrc: "0100000001" },
      replace: true
    });
    expect(JSON.stringify(result)).not.toMatch(/secret|password|token|sourceUrl/i);
  });

  it("maps node detail and config with a strict view allowlist", () => {
    expect(
      resolveLegacyMediaRoute("/gb28181/zlm/nodes/17", {
        view: "runtime",
        token: "discard"
      })
    ).toEqual({
      path: "/media/nodes/17",
      query: { view: "runtime" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/nodes/17", { view: "logs" })).toEqual({
      path: "/media/nodes/17",
      query: { view: "overview" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/config", {}, { recentNodeId: 8 })).toEqual({
      path: "/media/nodes/8",
      query: { view: "config" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/config")).toEqual({
      path: "/media/nodes",
      query: { intent: "config" },
      replace: true
    });
  });

  it("returns null for non-media and malformed legacy paths", () => {
    expect(resolveLegacyMediaRoute("/login", { nodeId: "2" })).toBeNull();
    expect(resolveLegacyMediaRoute("/gb28181/zlm/nodes/not-a-number")).toBeNull();
  });
});
