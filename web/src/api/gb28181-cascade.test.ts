import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  getCascadeBusinessGroup,
  getCascadeShares,
  listCascadePlatforms,
  replaceCascadeBusinessGroup,
  replaceCascadeShares,
  setCascadePlatformEnabled,
  updateCascadePlatform
} from "./gb28181";

describe("cascade API contract", () => {
  beforeEach(() => request.mockReset());

  it("uses the management endpoints and keeps password write-only", async () => {
    const input: any = { name: "upstream", password: "secret", enabled: false };
    await listCascadePlatforms();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/cascade/platforms");
    await updateCascadePlatform(7, input, 3);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/cascade/platforms/7", {
      data: { ...input, expectedRevision: 3 }
    });
    await setCascadePlatformEnabled(7, true, 4);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/cascade/platforms/7/enabled", {
      data: { enabled: true, expectedRevision: 4 }
    });
  });

  it("round-trips projection scope without adding unsupported sessions calls", async () => {
    await getCascadeShares(7);
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/cascade/platforms/7/shares");
    const data = {
      scope: "channels" as const,
      devices: [],
      channels: [
        {
          sourceDeviceId: 1,
          sourceChannelId: 2,
          publishedChannelId: "34020000001320000002",
          name: "channel",
          parentOverride: "",
          ptzAllowed: false
        }
      ],
      expectedProjectionRevision: 3
    };
    await replaceCascadeShares(7, data);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/cascade/platforms/7/shares", { data });
  });

  it("loads and replaces a platform business group with the projection revision", async () => {
    await getCascadeBusinessGroup(7);
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/cascade/platforms/7/business-group");

    const data = {
      nodes: [
        { code: "34020000002150000001", name: "运营目录", kind: "group" as const, parentCode: "34020000002000000001", sort: 0 }
      ],
      bindings: [{ sourceChannelId: 21, nodeCode: "34020000002150000001" }],
      expectedProjectionRevision: 12
    };
    await replaceCascadeBusinessGroup(7, data);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/cascade/platforms/7/business-group", { data });
  });
});
