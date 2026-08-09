import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  fetchPTZDefaultSpeedConfig,
  fetchSDPExtensionConfig,
  fetchSIPLogConfig,
  fetchPositionHistoryConfig,
  fetchSyncChannelsOnOnlineConfig,
  fetchIgnoreChannelOfflineStatusNotifyConfig,
  fetchOnlineOnHeartbeatConfig,
  fetchSaveAlarmMessagesConfig,
  fetchSIPCommandTimeoutConfig,
  fetchPreallocationModeConfig,
  getControlCapabilities,
  getDeviceStatus,
  getHomePosition,
  getPtzOperation,
  getStreamMonitor,
  listCruiseTracks,
  listPtzPresets,
  updateHomePosition,
  updatePositionHistoryConfig,
  updatePTZDefaultSpeedConfig,
  updateSDPExtensionConfig,
  updateSIPLogConfig,
  updateSyncChannelsOnOnlineConfig,
  updateIgnoreChannelOfflineStatusNotifyConfig,
  updateOnlineOnHeartbeatConfig,
  updateSaveAlarmMessagesConfig,
  updateSIPCommandTimeoutConfig,
  updatePreallocationModeConfig,
  type DeviceStatusResult,
  type HomePositionPatch
} from "./gb28181";

describe("国标服务配置 API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: { enabled: true, retentionDays: 7 } });
  });

  it("读取移动位置历史轨迹开关", async () => {
    await fetchPositionHistoryConfig();
    expect(request).toHaveBeenCalledWith("get", "/api/gb28181/sip/service-config/position-history");
  });

  it("同时提交轨迹开关和保留天数", async () => {
    await updatePositionHistoryConfig({ enabled: false, retentionDays: 30 });
    expect(request).toHaveBeenCalledWith(
      "put",
      "/api/gb28181/sip/service-config/position-history",
      { data: { enabled: false, retentionDays: 30 } }
    );
  });

  it("读取并更新扩展 SDP 兼容模式", async () => {
    await fetchSDPExtensionConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/sdp-extension");

    await updateSDPExtensionConfig(true);
    expect(request).toHaveBeenLastCalledWith(
      "put",
      "/api/gb28181/sip/service-config/sdp-extension",
      { data: { enabled: true } }
    );
  });

  it("读取并更新云台默认速度档位", async () => {
    await fetchPTZDefaultSpeedConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/ptz-default-speed");

    await updatePTZDefaultSpeedConfig(10);
    expect(request).toHaveBeenLastCalledWith(
      "put",
      "/api/gb28181/sip/service-config/ptz-default-speed",
      { data: { level: 10 } }
    );
  });

  it("读取并更新设备上线同步通道配置", async () => {
    await fetchSyncChannelsOnOnlineConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/sync-channels-on-online");

    await updateSyncChannelsOnOnlineConfig(false);
    expect(request).toHaveBeenLastCalledWith(
      "put",
      "/api/gb28181/sip/service-config/sync-channels-on-online",
      { data: { enabled: false } }
    );
  });

  it("读取并更新收到心跳恢复设备上线配置", async () => {
    await fetchOnlineOnHeartbeatConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/online-on-heartbeat");

    await updateOnlineOnHeartbeatConfig(false);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config/online-on-heartbeat", {
      data: { enabled: false }
    });
  });

  it("读取并更新报警消息存储配置", async () => {
    await fetchSaveAlarmMessagesConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/save-alarm-messages");

    await updateSaveAlarmMessagesConfig(false);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config/save-alarm-messages", {
      data: { enabled: false }
    });
  });

  it("读取并更新 SIP 命令超时时间", async () => {
    await fetchSIPCommandTimeoutConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/sip-command-timeout");

    await updateSIPCommandTimeoutConfig(30);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config/sip-command-timeout", {
      data: { timeoutSec: 30 }
    });
  });

  it("读取并更新预分配模式", async () => {
    await fetchPreallocationModeConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/preallocation-mode");

    await updatePreallocationModeConfig(true);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config/preallocation-mode", {
      data: { enabled: true }
    });
  });

  it("读取并更新忽略通道离线/异常通知配置", async () => {
    await fetchIgnoreChannelOfflineStatusNotifyConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/ignore-channel-offline-status-notify");

    await updateIgnoreChannelOfflineStatusNotifyConfig(true);
    expect(request).toHaveBeenLastCalledWith("put", "/api/gb28181/sip/service-config/ignore-channel-offline-status-notify", {
      data: { enabled: true }
    });
  });

  it("读取并更新 SIP 日志配置", async () => {
    await fetchSIPLogConfig();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/sip/service-config/sip-log");

    await updateSIPLogConfig(true);
    expect(request).toHaveBeenLastCalledWith(
      "put",
      "/api/gb28181/sip/service-config/sip-log",
      { data: { enabled: true } }
    );
  });
});

describe("gb28181 home position API", () => {
  beforeEach(() => {
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("uses cache and explicit refresh URLs without creating an implicit refresh", async () => {
    await getHomePosition(12);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      { params: undefined },
      { showErrorMessage: false }
    );

    await getHomePosition(12, true, "refresh-key");
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/home-position",
      {
        params: { refresh: true },
        headers: { "Idempotency-Key": "refresh-key" }
      },
      { showErrorMessage: false }
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
      "/api/gb28181/device-mgmt/channel/12/ptz/operations/home-op-1",
      undefined,
      { showErrorMessage: false }
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
      { params: undefined },
      { showErrorMessage: false }
    );

    await getDeviceStatus(12, true);
    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/device-status",
      { params: { refresh: true } },
      { showErrorMessage: false }
    );
  });

  it("silences all auxiliary reads started by the playback panel", async () => {
    await getStreamMonitor("stream-1");
    await getControlCapabilities(12);
    await listPtzPresets(12);
    await listCruiseTracks(12, true);

    expect(request).toHaveBeenNthCalledWith(
      1,
      "get",
      "/api/gb28181/play/stream-1/monitor",
      undefined,
      { showErrorMessage: false }
    );
    expect(request).toHaveBeenNthCalledWith(
      2,
      "get",
      "/api/gb28181/device-mgmt/channel/12/control-capabilities",
      undefined,
      { showErrorMessage: false }
    );
    expect(request).toHaveBeenNthCalledWith(
      3,
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/presets",
      { params: undefined },
      { showErrorMessage: false }
    );
    expect(request).toHaveBeenNthCalledWith(
      4,
      "get",
      "/api/gb28181/device-mgmt/channel/12/ptz/cruise-tracks",
      { params: { refresh: true } },
      { showErrorMessage: false }
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
