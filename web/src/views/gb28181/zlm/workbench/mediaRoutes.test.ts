import { describe, expect, it } from "vitest";

import { LEGACY_MEDIA_ROUTE_PATHS, MEDIA_NODE_DETAIL, MEDIA_PAGES, resolveLegacyMediaRoute } from "./mediaRoutes";

describe("media direct-page route contract", () => {
  it("defines ten ordered direct pages with one merged overview", () => {
    expect(MEDIA_PAGES.map(item => [item.path, item.title, item.sort])).toEqual([
      ["/gb28181/zlm/overview", "总览", 10],
      ["/gb28181/zlm/nodes", "节点管理", 20],
      ["/gb28181/zlm/streams", "流管理", 40],
      ["/gb28181/zlm/sessions", "会话管理", 50],
      ["/gb28181/zlm/proxies", "拉流/推流代理", 60],
      ["/gb28181/zlm/ffmpeg-sources", "FFmpeg 源", 70],
      ["/gb28181/zlm/rtp-servers", "RTP 服务", 80],
      ["/gb28181/zlm/config", "服务器配置", 90],
      ["/gb28181/zlm/scheduler", "调度策略", 100],
      ["/gb28181/zlm/scheduler/logs", "调度日志", 110]
    ]);
    expect(new Set(MEDIA_PAGES.map(item => item.path)).size).toBe(10);
    expect(MEDIA_PAGES.map(item => item.path)).not.toContain("/gb28181/zlm/runtime");
    expect(MEDIA_PAGES.map(item => item.path)).not.toContain("/gb28181/cloud-recordings");
    expect(MEDIA_PAGES.map(item => item.path)).not.toContain("/gb28181/recording-schedules");
    expect(MEDIA_NODE_DETAIL).toMatchObject({
      path: "/gb28181/zlm/nodes/:id",
      defaultView: "overview",
      allowedViews: ["overview", "runtime", "config"]
    });
  });

  it("keeps all former addresses as compatibility aliases", () => {
    expect(LEGACY_MEDIA_ROUTE_PATHS).toHaveLength(14);
    expect(new Set(LEGACY_MEDIA_ROUTE_PATHS).size).toBe(14);

    const destinations = LEGACY_MEDIA_ROUTE_PATHS.map(path => resolveLegacyMediaRoute(path.replace(":id", "23")));

    expect(destinations.every(location => location?.replace)).toBe(true);
    expect(destinations.every(location => location?.path.startsWith("/media/") || location?.path.startsWith("/gb28181/"))).toBe(true);
  });

  it("maps the six V2 workspaces back to direct pages", () => {
    expect(resolveLegacyMediaRoute("/media/overview")).toMatchObject({ path: "/gb28181/zlm/overview" });
    expect(resolveLegacyMediaRoute("/media/monitoring", { view: "sessions", nodeId: 2 })).toEqual({
      path: "/gb28181/zlm/sessions", query: { nodeId: "2" }, replace: true
    });
    expect(resolveLegacyMediaRoute("/media/ingress", { view: "rtp", nodeId: 3 })).toEqual({
      path: "/gb28181/zlm/rtp-servers", query: { nodeId: "3" }, replace: true
    });
    expect(resolveLegacyMediaRoute("/media/recordings", { view: "plans" })).toMatchObject({ path: "/gb28181/recording-schedules" });
    expect(resolveLegacyMediaRoute("/media/scheduling", { view: "logs" })).toMatchObject({ path: "/gb28181/zlm/scheduler/logs" });
  });

  it("maps the retired runtime page into the merged overview and preserves a valid node", () => {
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime", { nodeId: "2" })).toEqual({
      path: "/gb28181/zlm/overview",
      query: { nodeId: "2" },
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime", { nodeId: "0" })).toEqual({
      path: "/gb28181/zlm/overview",
      query: {},
      replace: true
    });
    expect(resolveLegacyMediaRoute("/gb28181/zlm/runtime")).toEqual({
      path: "/gb28181/zlm/overview",
      query: {},
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
