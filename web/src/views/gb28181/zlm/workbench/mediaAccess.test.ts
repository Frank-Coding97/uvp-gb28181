import { describe, expect, it } from "vitest";

import { resolveFirstMediaWorkspace, resolveMediaWorkspaceAccess } from "./mediaAccess";

describe("media workbench access contract", () => {
  it("derives workspace and view visibility from legacy route unions", () => {
    const streams = resolveMediaWorkspaceAccess(["/gb28181/zlm/streams"]);
    expect(streams.workspacePaths).toEqual(["/media/monitoring"]);
    expect(streams.viewsByWorkspace["/media/monitoring"]).toEqual(["streams"]);

    const sessions = resolveMediaWorkspaceAccess(["/gb28181/zlm/sessions"]);
    expect(sessions.workspacePaths).toEqual(["/media/monitoring"]);
    expect(sessions.viewsByWorkspace["/media/monitoring"]).toEqual(["sessions"]);

    const config = resolveMediaWorkspaceAccess(["/gb28181/zlm/config"]);
    expect(config.workspacePaths).toEqual(["/media/nodes"]);
    expect(config.viewsByWorkspace["/media/nodes"]).toEqual(["config"]);
  });

  it("does not expand one legacy capability into sibling views", () => {
    const access = resolveMediaWorkspaceAccess([
      "/gb28181/zlm/streams",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/recording-schedules",
      "/gb28181/zlm/scheduler/logs"
    ]);

    expect(access.viewsByWorkspace["/media/monitoring"]).toEqual(["streams"]);
    expect(access.viewsByWorkspace["/media/ingress"]).toEqual(["ffmpeg"]);
    expect(access.viewsByWorkspace["/media/recordings"]).toEqual(["plans"]);
    expect(access.viewsByWorkspace["/media/scheduling"]).toEqual(["logs"]);
  });

  it("grants all six workspaces and views only to an explicit wildcard admin", () => {
    const access = resolveMediaWorkspaceAccess([], { wildcard: true });

    expect(access.workspacePaths).toEqual([
      "/media/overview",
      "/media/monitoring",
      "/media/ingress",
      "/media/recordings",
      "/media/nodes",
      "/media/scheduling"
    ]);
    expect(access.viewsByWorkspace["/media/ingress"]).toEqual(["pull", "push", "ffmpeg", "rtp"]);
    expect(access.viewsByWorkspace["/media/nodes"]).toEqual(["list", "overview", "runtime", "config"]);
  });

  it("selects the first accessible workspace instead of assuming overview", () => {
    expect(resolveFirstMediaWorkspace(["/gb28181/zlm/rtp-servers"])).toBe("/media/ingress");
    expect(resolveFirstMediaWorkspace(["/gb28181/zlm/scheduler/logs"])).toBe("/media/scheduling");
    expect(resolveFirstMediaWorkspace([])).toBeNull();
  });

  it("normalizes dynamic node detail and ignores unrelated paths", () => {
    const access = resolveMediaWorkspaceAccess(["/gb28181/zlm/nodes/:id", "/gb28181/device-mgmt/devices", "/media/monitoring"]);

    expect(access.workspacePaths).toEqual(["/media/nodes"]);
    expect(access.viewsByWorkspace["/media/nodes"]).toEqual(["overview", "runtime", "config"]);
  });
});
