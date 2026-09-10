import { describe, expect, it } from "vitest";
import { cascadeLocalIdentityDefaults, cascadePresentation, defaultCascadePlatform, resolveChannelPTZAllowed, validateCascadePlatform } from "./cascadeState";

describe("cascade platform state", () => {
  it("maps runtime facts to product conclusions", () => {
    expect(cascadePresentation({ enabled: false, overall: "offline", registration: "unregistered", heartbeat: "unknown" })).toMatchObject({ label: "已停用", color: "gray" });
    expect(cascadePresentation({ enabled: true, overall: "online", registration: "registered", heartbeat: "healthy" })).toMatchObject({ label: "在线", color: "green" });
    expect(cascadePresentation({ enabled: true, overall: "offline", registration: "expired", heartbeat: "stale" })).toMatchObject({ label: "注册已过期", color: "red" });
  });

  it("rejects invalid identities and ports without leaking backend diagnostics", () => {
    const errors = validateCascadePlatform({ ...defaultCascadePlatform(), name: "", upstreamServerId: "1", host: "bad host", port: 0, localDeviceId: "2", localDomain: "", localSipIp: "not an ip", localSipPort: 70000 });
    expect(errors).toEqual(expect.arrayContaining(["平台名称不能为空", "上级平台 ID 必须是 20 位数字", "上级端口必须在 1-65535 之间", "本平台设备 ID 必须是 20 位数字"]));
    expect(errors.length).toBeGreaterThan(4);
  });

  it("maps the configured local SIP identity into a new cascade platform", () => {
    expect(cascadeLocalIdentityDefaults({
      listenIp: "0.0.0.0",
      advertiseIp: "192.168.10.106",
      port: 5061,
      domain: "3402000000",
      serverId: "34020000001320000001"
    })).toEqual({
      localDeviceId: "34020000001320000001",
      localDomain: "3402000000",
      localSipIp: "192.168.10.106",
      localSipPort: 5061,
      mediaAdvertiseIp: "192.168.10.106"
    });
  });

  it("does not use a wildcard listener as the local advertised address", () => {
    expect(cascadeLocalIdentityDefaults({
      listenIp: "0.0.0.0",
      advertiseIp: "",
      port: 5060,
      domain: "3402000000",
      serverId: "34020000001320000001"
    }).localSipIp).toBe("");
  });

  it("defaults new shared channels to the platform PTZ setting", () => {
    expect(resolveChannelPTZAllowed(undefined, true)).toBe(true);
    expect(resolveChannelPTZAllowed(undefined, false)).toBe(false);
  });

  it("preserves an existing per-channel PTZ setting", () => {
    expect(resolveChannelPTZAllowed(false, true)).toBe(false);
    expect(resolveChannelPTZAllowed(true, false)).toBe(true);
  });

});
