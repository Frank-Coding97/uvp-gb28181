import { describe, expect, it } from "vitest";

import { resolveFirstMediaWorkspace, resolveMediaWorkspaceAccess } from "./mediaAccess";

describe("media workbench access contract", () => {
  it("derives workspace and view visibility from legacy route unions", () => {
    const streams = resolveMediaWorkspaceAccess(["/gb28181/zlm/streams"]);
    expect(streams.workspacePaths).toEqual(["/gb28181/zlm/streams"]);
    expect(streams.viewsByWorkspace["/gb28181/zlm/streams"]).toEqual(["default"]);

    const sessions = resolveMediaWorkspaceAccess(["/gb28181/zlm/sessions"]);
    expect(sessions.workspacePaths).toEqual(["/gb28181/zlm/sessions"]);
    expect(sessions.viewsByWorkspace["/gb28181/zlm/sessions"]).toEqual(["default"]);

    const config = resolveMediaWorkspaceAccess(["/gb28181/zlm/config"]);
    expect(config.workspacePaths).toEqual(["/gb28181/zlm/config"]);
    expect(config.viewsByWorkspace["/gb28181/zlm/config"]).toEqual(["default"]);
  });

  it("does not expand one legacy capability into sibling views", () => {
    const access = resolveMediaWorkspaceAccess([
      "/gb28181/zlm/streams",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/recording-schedules",
      "/gb28181/zlm/scheduler/logs"
    ]);

    expect(access.workspacePaths).toEqual([
      "/gb28181/zlm/streams",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/zlm/scheduler/logs"
    ]);
    expect(access.workspacePaths).not.toContain("/gb28181/recording-schedules");
  });

  it("grants all ten media pages only to an explicit wildcard admin", () => {
    const access = resolveMediaWorkspaceAccess([], { wildcard: true });

    expect(access.workspacePaths).toEqual([
      "/gb28181/zlm/overview",
      "/gb28181/zlm/nodes",
      "/gb28181/zlm/streams",
      "/gb28181/zlm/sessions",
      "/gb28181/zlm/proxies",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/zlm/rtp-servers",
      "/gb28181/zlm/config",
      "/gb28181/zlm/scheduler",
      "/gb28181/zlm/scheduler/logs"
    ]);
    expect(access.viewsByWorkspace["/gb28181/zlm/proxies"]).toEqual(["default"]);
  });

  it("selects the first accessible workspace instead of assuming overview", () => {
    expect(resolveFirstMediaWorkspace(["/gb28181/zlm/rtp-servers"])).toBe("/gb28181/zlm/rtp-servers");
    expect(resolveFirstMediaWorkspace(["/gb28181/zlm/scheduler/logs"])).toBe("/gb28181/zlm/scheduler/logs");
    expect(resolveFirstMediaWorkspace([])).toBeNull();
  });

  it("normalizes dynamic node detail and ignores unrelated paths", () => {
    const access = resolveMediaWorkspaceAccess(["/gb28181/zlm/nodes/:id", "/gb28181/device-mgmt/devices", "/media/monitoring"]);

    expect(access.workspacePaths).toEqual(["/gb28181/zlm/nodes"]);
    expect(access.viewsByWorkspace["/gb28181/zlm/nodes"]).toEqual(["default"]);
  });

  it("keeps retired V2 workspace view grants usable during rolling deployment", () => {
    const access = resolveMediaWorkspaceAccess([
      "/gb28181/zlm/streams",
      "/gb28181/zlm/sessions",
      "/gb28181/zlm/proxies",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/zlm/rtp-servers"
    ]);

    expect(access.viewsByWorkspace["/media/monitoring"]).toEqual(["streams", "sessions"]);
    expect(access.viewsByWorkspace["/media/ingress"]).toEqual(["pull", "push", "ffmpeg", "rtp"]);
  });
});
