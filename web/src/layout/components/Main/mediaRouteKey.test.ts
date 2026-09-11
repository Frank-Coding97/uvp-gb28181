import { describe, expect, it } from "vitest";

import { resolveMediaRouteRenderKey } from "./mediaRouteKey";

function route(path: string, fullPath: string, matchedPath: string = path) {
  return { path, fullPath, matched: [{ path: matchedPath }] };
}

describe("media route render key", () => {
  it("keeps each direct media page mounted while its query changes", () => {
    const paths = [
      "/gb28181/zlm/overview", "/gb28181/zlm/nodes", "/gb28181/zlm/runtime",
      "/gb28181/zlm/streams", "/gb28181/zlm/sessions", "/gb28181/zlm/proxies",
      "/gb28181/zlm/ffmpeg-sources", "/gb28181/zlm/rtp-servers", "/gb28181/zlm/config",
      "/gb28181/zlm/scheduler", "/gb28181/zlm/scheduler/logs"
    ];

    for (const path of paths) {
      expect(resolveMediaRouteRenderKey(route(path, `${path}?view=one`))).toBe(path);
      expect(resolveMediaRouteRenderKey(route(path, `${path}?view=two`))).toBe(path);
    }
  });

  it("uses the matched node-detail pattern across id and view changes", () => {
    expect(resolveMediaRouteRenderKey(route("/gb28181/zlm/nodes/2", "/gb28181/zlm/nodes/2?view=runtime", "/gb28181/zlm/nodes/:id"))).toBe(
      "/gb28181/zlm/nodes/:id"
    );
    expect(resolveMediaRouteRenderKey(route("/gb28181/zlm/nodes/9", "/gb28181/zlm/nodes/9?view=config", "/gb28181/zlm/nodes/:id"))).toBe(
      "/gb28181/zlm/nodes/:id"
    );
  });

  it("preserves fullPath behavior for non-media routes", () => {
    expect(resolveMediaRouteRenderKey(route("/gb28181/device-mgmt/devices", "/gb28181/device-mgmt/devices?page=2"))).toBe(
      "/gb28181/device-mgmt/devices?page=2"
    );
    expect(resolveMediaRouteRenderKey(route("/gb28181/zlm/streams", "/gb28181/zlm/streams?nodeId=2"))).toBe(
      "/gb28181/zlm/streams"
    );
  });

  it("falls back safely when matched records are unavailable", () => {
    expect(
      resolveMediaRouteRenderKey({
        path: "/gb28181/zlm/nodes/12",
        fullPath: "/gb28181/zlm/nodes/12?view=overview"
      })
    ).toBe("/gb28181/zlm/nodes/:id");
  });

  it("keeps the retired V2 node-detail bridge stable during rolling deployment", () => {
    expect(resolveMediaRouteRenderKey(route("/media/nodes/2", "/media/nodes/2?view=runtime", "/media/nodes/:id"))).toBe(
      "/media/nodes/:id"
    );
  });
});
