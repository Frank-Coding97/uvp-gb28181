import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  getDeviceStatus,
  getHomePosition,
  getPtzOperation,
  updateHomePosition,
  type DeviceStatusResult,
  type HomePositionPatch
} from "./gb28181";

describe("gb28181 home position API", () => {
  beforeEach(() => {
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("uses cache and explicit refresh URLs without creating an implicit refresh", async () => {
    await getHomePosition(12);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      { params: undefined }
    );

    await getHomePosition(12, true, "refresh-key");
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      {
        params: { refresh: true },
        headers: { "Idempotency-Key": "refresh-key" }
      }
    );
  });

  it("keeps preset zero and sends idempotency only in the header", async () => {
    await updateHomePosition(
      12,
      { enabled: true, resetTime: 10, presetId: 0 },
      "control-key"
    );

    expect(request).toHaveBeenCalledWith(
      "patch",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      {
        data: { enabled: true, resetTime: 10, presetId: 0 },
        headers: { "Idempotency-Key": "control-key" }
      }
    );
  });

  it("normalizes a disable request to enabled only", async () => {
    await updateHomePosition(12, { enabled: false }, "disable-key");

    expect(request).toHaveBeenCalledWith(
      "patch",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      {
        data: { enabled: false },
        headers: { "Idempotency-Key": "disable-key" }
      }
    );
  });

  it("loads the exact operation for the channel", async () => {
    await getPtzOperation(12, "home-op-1");

    expect(request).toHaveBeenCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/operations/home-op-1"
    );
  });

  it("exposes a discriminated update payload", () => {
    const enabled: HomePositionPatch = {
      enabled: true,
      resetTime: 10,
      presetId: 0
    };
    const disabled: HomePositionPatch = { enabled: false };
    expect([enabled, disabled]).toHaveLength(2);

    if (false) {
      // @ts-expect-error enabled requests require resetTime and presetId
      updateHomePosition(12, { enabled: true });
      // @ts-expect-error disabled requests cannot carry enabled-only fields
      updateHomePosition(12, { enabled: false, resetTime: 10, presetId: 1 });
    }
  });
});

describe("gb28181 device status API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: { freshness: "unknown" } });
  });

  it("keeps DeviceStatus refresh asynchronous and sends the exact channel URL", async () => {
    await getDeviceStatus(12);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/device-status",
      { params: undefined }
    );

    await getDeviceStatus(12, true);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/device-status",
      { params: { refresh: true } }
    );
  });

  it("exposes independent record and alarm refresh operations and ALARM facts", () => {
    const result: DeviceStatusResult = {
      state: { recordState: "on", guardState: "alarm", freshness: "fresh" },
      recordState: "on",
      guardState: "alarm",
      freshness: "fresh",
      completeness: "complete",
      record: {
        state: "on",
        freshness: "fresh",
        targetScope: "channel",
        targetCode: "C1"
      },
      alarmResolution: {
        status: "resolved",
        source: "direct_parent",
        targetCode: "A1",
        state: "alarm",
        freshness: "fresh",
        candidates: [{ code: "A1", name: "门磁" }]
      },
      alarmFacts: [{ targetCode: "A1", guardState: "alarm", freshness: "fresh" }],
      refreshOperationId: "record-op",
      recordRefreshOperationId: "record-op",
      alarmRefreshOperationId: "alarm-op",
      refreshOperationIds: { record: "record-op", alarm: "alarm-op" }
    };

    expect(result.refreshOperationIds).toEqual({ record: "record-op", alarm: "alarm-op" });
    expect(result.alarmFacts?.[0].guardState).toBe("alarm");
  });

  it("allows DeviceStatus facts to be absent instead of coercing them to false", () => {
    const result: DeviceStatusResult = {
      state: { freshness: "fresh" },
      freshness: "fresh"
    };

    expect(result.state?.recordState).toBeUndefined();
    expect(result.state?.guardState).toBeUndefined();
  });
});
