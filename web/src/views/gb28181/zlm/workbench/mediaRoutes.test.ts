import { describe, expect, it } from "vitest";

import { MEDIA_WORKSPACES } from "./mediaRoutes";

describe("media workspace route contract", () => {
  it("defines the five canonical workbench entries in sidebar order", () => {
    expect(MEDIA_WORKSPACES.map(item => [item.key, item.title, item.path, item.sort])).toEqual([
      ["overview", "运行总览", "/media/overview", 10],
      ["monitoring", "流与会话", "/media/monitoring", 20],
      ["ingress", "接入管理", "/media/ingress", 30],
      ["nodes", "节点管理", "/media/nodes", 40],
      ["scheduling", "调度管理", "/media/scheduling", 50]
    ]);
    expect(new Set(MEDIA_WORKSPACES.map(item => item.path)).size).toBe(5);
  });

  it("keeps only /media addresses and never reintroduces a retired compatibility path", () => {
    const paths = MEDIA_WORKSPACES.map(item => item.path);

    for (const path of paths) {
      expect(path.startsWith("/media/")).toBe(true);
    }
    expect(paths).not.toContain("/media/recordings");
    expect(paths.some(path => path.startsWith("/gb28181/zlm/"))).toBe(false);
    expect(MEDIA_WORKSPACES.map(item => item.key)).not.toContain("recordings");
  });

  it("declares a default view that belongs to its own allowed view list", () => {
    for (const workspace of MEDIA_WORKSPACES) {
      expect(workspace.allowedViews, workspace.key).toContain(workspace.defaultView);
      expect(workspace.allowedViews.length, workspace.key).toBeGreaterThan(0);
    }
  });
});
