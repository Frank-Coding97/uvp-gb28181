import { describe, expect, it } from "vitest";

import { resolveFirstMediaWorkspace, resolveMediaWorkspaceAccess } from "./mediaAccess";

describe("media workbench access contract", () => {
  it("grants a whole workspace once its sidebar menu is visible", () => {
    const monitoring = resolveMediaWorkspaceAccess(["/media/monitoring"]);
    expect(monitoring.workspacePaths).toEqual(["/media/monitoring"]);
    expect(monitoring.viewsByWorkspace["/media/monitoring"]).toEqual(["streams", "sessions", "viewers"]);

    const ingress = resolveMediaWorkspaceAccess(["/media/ingress"]);
    expect(ingress.workspacePaths).toEqual(["/media/ingress"]);
    expect(ingress.viewsByWorkspace["/media/ingress"]).toEqual(["pull", "push", "ffmpeg", "rtp"]);
  });

  it("keeps sidebar order regardless of the grant order", () => {
    const access = resolveMediaWorkspaceAccess(["/media/scheduling", "/media/overview", "/media/ingress"]);

    expect(access.workspacePaths).toEqual(["/media/overview", "/media/ingress", "/media/scheduling"]);
  });

  it("grants all five workspaces only to an explicit wildcard admin", () => {
    const access = resolveMediaWorkspaceAccess([], { wildcard: true });

    expect(access.workspacePaths).toEqual([
      "/media/overview",
      "/media/monitoring",
      "/media/ingress",
      "/media/nodes",
      "/media/scheduling"
    ]);
    expect(access.viewsByWorkspace["/media/ingress"]).toEqual(["pull", "push", "ffmpeg", "rtp"]);
  });

  it("selects the first accessible workspace instead of assuming overview", () => {
    expect(resolveFirstMediaWorkspace(["/media/ingress"])).toBe("/media/ingress");
    expect(resolveFirstMediaWorkspace(["/media/scheduling"])).toBe("/media/scheduling");
    expect(resolveFirstMediaWorkspace([])).toBeNull();
  });

  it("grants only the log view from the scheduling menu", () => {
    const access = resolveMediaWorkspaceAccess(["/media/scheduling"]);

    expect(access.viewsByWorkspace["/media/scheduling"]).toEqual(["logs"]);
  });

  it("resolves the hidden node detail route to its parent workspace and ignores unrelated paths", () => {
    const access = resolveMediaWorkspaceAccess(["/media/nodes/:id", "/gb28181/device-mgmt/devices"]);

    expect(access.workspacePaths).toEqual(["/media/nodes"]);
    expect(access.viewsByWorkspace["/media/nodes"]).toEqual(["list", "overview", "runtime", "config"]);
  });

  it("ignores the retired compatibility addresses entirely", () => {
    const access = resolveMediaWorkspaceAccess([
      "/gb28181/zlm/streams",
      "/gb28181/zlm/config",
      "/gb28181/zlm/nodes/:id",
      "/media/recordings"
    ]);

    expect(access.workspacePaths).toEqual([]);
    expect(access.viewsByWorkspace).toEqual({});
  });
});
