import { describe, expect, it } from "vitest";
import { cascadeCycleLabel, cascadeFormFieldErrors, cascadeLocalIdentityDefaults, cascadePresentation, defaultCascadePlatform, resolveChannelPTZAllowed, validateCascadePlatform } from "./cascadeState";

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
      mediaAdvertiseIp: "192.168.10.106",
      authUsername: "34020000001320000001"
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

  it("renders register/keepalive cycles in plain seconds", () => {
    expect(cascadeCycleLabel({ registerExpires: 3600, keepaliveInterval: 60 })).toBe("3600 / 60");
    expect(cascadeCycleLabel({ registerExpires: 7200, keepaliveInterval: 45 })).toBe("7200 / 45");
  });

  it("falls back to backend default cycles when unconfigured", () => {
    expect(cascadeCycleLabel({ registerExpires: 0, keepaliveInterval: 0 })).toBe("3600 / 60");
  });

  it("hides per-field errors until the field is touched", () => {
    const form = { ...defaultCascadePlatform(), upstreamServerId: "123" };
    const untouched = cascadeFormFieldErrors(form, {});
    expect(untouched.upstreamServerId).toBe("");
    expect(untouched.name).toBe("");

    const touched = cascadeFormFieldErrors(form, { name: true, upstreamServerId: true });
    expect(touched.name).toBe("平台名称不能为空");
    expect(touched.upstreamServerId).toBe("必须是 20 位数字编码");
  });

  it("reports valid touched fields as empty and checks optional advertise address", () => {
    const form = { ...defaultCascadePlatform(), name: "上级", upstreamServerId: "34020000002000000001", localDeviceId: "34020000001320000001", mediaAdvertiseIp: "bad ip" };
    const touched = { name: true, upstreamServerId: true, localDeviceId: true, mediaAdvertiseIp: true };
    const errors = cascadeFormFieldErrors(form, touched);
    expect(errors.name).toBe("");
    expect(errors.upstreamServerId).toBe("");
    expect(errors.localDeviceId).toBe("");
    expect(errors.mediaAdvertiseIp).toBe("地址格式不正确");
  });

});
