import { describe, expect, it } from "vitest";

import { resolveMediaWorkbenchTabGroup } from "@/router/media-route-identity";
import { resolveMediaRouteRenderKey } from "./mediaRouteKey";

function route(path: string, fullPath: string, matchedPath: string = path) {
  return { path, fullPath, matched: [{ path: matchedPath }] };
}

const WORKBENCH_PATHS = [
  "/media/overview",
  "/media/monitoring",
  "/media/ingress",
  "/media/nodes",
  "/media/scheduling"
];

describe("media route render key", () => {
  it("keeps each workbench page mounted while its query changes", () => {
    for (const path of WORKBENCH_PATHS) {
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

  it("preserves fullPath behavior for non-media routes", () => {
    expect(resolveMediaRouteRenderKey(route("/gb28181/device-mgmt/devices", "/gb28181/device-mgmt/devices?page=2"))).toBe(
      "/gb28181/device-mgmt/devices?page=2"
    );
    expect(resolveMediaRouteRenderKey(route("/media/monitoring", "/media/monitoring?nodeId=2"))).toBe(
      "/media/monitoring"
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

  it("no longer treats retired /gb28181/zlm addresses as stable identities", () => {
    expect(resolveMediaRouteRenderKey(route("/gb28181/zlm/streams", "/gb28181/zlm/streams?nodeId=2"))).toBe(
      "/gb28181/zlm/streams?nodeId=2"
    );
    expect(resolveMediaRouteRenderKey(route("/media/recordings", "/media/recordings?view=files"))).toBe(
      "/media/recordings?view=files"
    );
  });

  it("gives each canonical workbench menu its own outer tab", () => {
    for (const path of WORKBENCH_PATHS) {
      expect(resolveMediaWorkbenchTabGroup(path), path).toBe(path);
    }
    expect(resolveMediaWorkbenchTabGroup("/media/nodes/7?view=runtime")).toBe("/media/nodes");
    expect(resolveMediaWorkbenchTabGroup("/gb28181/zlm/streams")).toBeNull();
    expect(resolveMediaWorkbenchTabGroup("/gb28181/device-mgmt/devices")).toBeNull();
  });
});
