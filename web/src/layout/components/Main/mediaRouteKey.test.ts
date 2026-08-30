import { describe, expect, it } from "vitest";

import { resolveMediaRouteRenderKey } from "./mediaRouteKey";

function route(path: string, fullPath: string, matchedPath: string = path) {
  return { path, fullPath, matched: [{ path: matchedPath }] };
}

describe("media route render key", () => {
  it("keeps each canonical workspace mounted while its query changes", () => {
    const paths = [
      "/media/overview",
      "/media/monitoring",
      "/media/ingress",
      "/media/recordings",
      "/media/nodes",
      "/media/scheduling"
    ];

    for (const path of paths) {
      expect(resolveMediaRouteRenderKey(route(path, `${path}?view=one`))).toBe(path);
      expect(resolveMediaRouteRenderKey(route(path, `${path}?view=two`))).toBe(path);
    }
  });

  it("uses the matched node-detail pattern across id and view changes", () => {
    expect(resolveMediaRouteRenderKey(route("/media/nodes/2", "/media/nodes/2?view=runtime", "/media/nodes/:id"))).toBe(
      "/media/nodes/:id"
    );
    expect(resolveMediaRouteRenderKey(route("/media/nodes/9", "/media/nodes/9?view=config", "/media/nodes/:id"))).toBe(
      "/media/nodes/:id"
    );
  });

  it("preserves fullPath behavior for non-workbench and legacy routes", () => {
    expect(resolveMediaRouteRenderKey(route("/gb28181/device-mgmt/devices", "/gb28181/device-mgmt/devices?page=2"))).toBe(
      "/gb28181/device-mgmt/devices?page=2"
    );
    expect(resolveMediaRouteRenderKey(route("/gb28181/zlm/streams", "/gb28181/zlm/streams?nodeId=2"))).toBe(
      "/gb28181/zlm/streams?nodeId=2"
    );
  });

  it("falls back safely when matched records are unavailable", () => {
    expect(
      resolveMediaRouteRenderKey({
        path: "/media/nodes/12",
        fullPath: "/media/nodes/12?view=overview"
      })
    ).toBe("/media/nodes/:id");
  });
});
