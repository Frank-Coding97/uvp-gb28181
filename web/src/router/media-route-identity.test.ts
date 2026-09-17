import { describe, expect, it } from "vitest";

import { resolveMediaRouteRenderKey, resolveMediaWorkbenchTabGroup } from "./media-route-identity";

describe("media route identity", () => {
  it.each([
    "/media/overview",
    "/media/monitoring",
    "/media/ingress",
    "/media/nodes",
    "/media/scheduling"
  ])("keeps %s in its own workbench tab", path => {
    expect(resolveMediaWorkbenchTabGroup(path)).toBe(path);
  });

  it("keeps node details in the node-management tab", () => {
    expect(resolveMediaWorkbenchTabGroup("/media/nodes/2")).toBe("/media/nodes");
  });

  it("keeps cloud recordings outside the ZL workbench tab", () => {
    expect(resolveMediaWorkbenchTabGroup("/media/recordings")).toBeNull();
  });

  it("ignores query changes when resolving a workbench render key", () => {
    expect(resolveMediaRouteRenderKey({
      path: "/media/ingress",
      fullPath: "/media/ingress?view=push&nodeId=2"
    })).toBe("/media/ingress");
  });
});
