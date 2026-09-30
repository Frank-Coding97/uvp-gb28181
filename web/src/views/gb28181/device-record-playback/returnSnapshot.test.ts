import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { consumeDeviceMgmtReturnSnapshot, saveDeviceMgmtReturnSnapshot, type DeviceMgmtReturnSnapshot } from "./returnSnapshot";

const snapshot: DeviceMgmtReturnSnapshot = {
  version: 1,
  viewMode: "list",
  assetKind: "channel",
  keyword: "东门",
  deviceIdFilter: "34020000002000000001",
  statusFilter: "online",
  directoryView: "administrative",
  directorySelectedKey: "administrative:340200",
  page: 2,
  listPageSize: 20,
  cardPageSize: 12
};

describe("device management return snapshot", () => {
  beforeEach(() => window.sessionStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("saves and consumes a snapshot exactly once", () => {
    const key = saveDeviceMgmtReturnSnapshot(snapshot, "fixed-key");
    expect(key).toBe("fixed-key");
    expect(consumeDeviceMgmtReturnSnapshot(key)).toEqual(snapshot);
    expect(consumeDeviceMgmtReturnSnapshot(key)).toBeNull();
  });

  it("ignores corrupt and unsupported snapshots", () => {
    window.sessionStorage.setItem("uvp.device-record-return:bad", "{");
    window.sessionStorage.setItem("uvp.device-record-return:old", JSON.stringify({ ...snapshot, version: 0 }));
    expect(consumeDeviceMgmtReturnSnapshot("bad")).toBeNull();
    expect(consumeDeviceMgmtReturnSnapshot("old")).toBeNull();
  });

  it("derives the key from crypto.randomUUID in a secure context", () => {
    vi.stubGlobal("crypto", { randomUUID: () => "1f9a6c1e-0000-4000-8000-000000000000" });

    const key = saveDeviceMgmtReturnSnapshot(snapshot);

    expect(key).toBe("1f9a6c1e-0000-4000-8000-000000000000");
    expect(consumeDeviceMgmtReturnSnapshot(key)).toEqual(snapshot);
  });

  // ⛔⛔ 回归锚点：「查询设备录像」里本函数在 router.push 的**前一行**，
  // 一抛错就整条中断点击处理 —— 现象是按钮「点了没反应」。
  // crypto.randomUUID 是 Secure Context 限定 API：http + 局域网 IP / 域名访问时
  // window.isSecureContext === false，crypto.randomUUID 是 undefined，原来直接调用必抛 TypeError。
  it("still returns a usable key when crypto.randomUUID is unavailable (http on a LAN host)", () => {
    vi.stubGlobal("crypto", {});

    const key = saveDeviceMgmtReturnSnapshot(snapshot);

    expect(typeof key).toBe("string");
    expect(key.length).toBeGreaterThan(0);
    expect(consumeDeviceMgmtReturnSnapshot(key)).toEqual(snapshot);
  });

  it("never reuses the degraded key, so two visits cannot overwrite each other", () => {
    vi.stubGlobal("crypto", {});

    const keys = new Set([saveDeviceMgmtReturnSnapshot(snapshot), saveDeviceMgmtReturnSnapshot(snapshot)]);

    expect(keys.size).toBe(2);
  });
});
