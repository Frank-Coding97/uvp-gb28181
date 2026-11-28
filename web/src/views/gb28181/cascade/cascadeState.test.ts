import { describe, expect, it } from "vitest";
import {
  cascadeRuntimeSyncPending,
  CASCADE_CATALOG_BATCH_SIZES,
  CASCADE_REGISTER_STATE_LABEL_FALLBACK,
  DICT_CODE_CASCADE_REGISTER_STATE,
  cascadeCycleLabel,
  cascadeFormFieldErrors,
  cascadeLocalIdentityDefaults,
  cascadePresentation,
  cascadeRegisterStateKey,
  channelSourceLabel,
  defaultCascadePlatform,
  pendingSourceChannelIds,
  resolveChannelPTZAllowed,
  validateCascadeStep,
  validateCascadePlatform
} from "./cascadeState";

describe("cascade platform state", () => {
  it("recognizes both direct and wrapped runtime-sync failure responses", () => {
    expect(cascadeRuntimeSyncPending({ runtimeSynced: false })).toBe(true);
    expect(cascadeRuntimeSyncPending({ platform: { runtimeSynced: false }, warning: "pending" })).toBe(true);
    expect(cascadeRuntimeSyncPending({ runtimeSynced: true })).toBe(false);
    expect(cascadeRuntimeSyncPending({})).toBe(false);
    expect(cascadeRuntimeSyncPending(null)).toBe(false);
  });

  it("keeps automatic catalog publication, recording authorization and RTCP opt-in", () => {
    expect(defaultCascadePlatform()).toMatchObject({
      autoPushCatalog: false,
      recordingEnabled: false,
      rtcpKeepalive: false,
      configEnabled: false,
      upgradeEnabled: false,
      deviceControlEnabled: false,
      maintenanceEnabled: false
    });
  });
  it("defaults catalog batches to one and limits new choices to powers of two through 64", () => {
    expect(defaultCascadePlatform().catalogBatchSize).toBe(1);
    expect(CASCADE_CATALOG_BATCH_SIZES).toEqual([1, 2, 4, 8, 16, 32, 64]);
    const form = defaultCascadePlatform();
    for (const catalogBatchSize of [1, 2, 4, 8, 16, 32, 64]) {
      expect(validateCascadeStep({ ...form, catalogBatchSize }, 4)).toEqual([]);
    }
    for (const catalogBatchSize of [0, -1, 1.5, 3, 65, 100]) {
      expect(validateCascadeStep({ ...form, catalogBatchSize }, 4)).not.toEqual([]);
    }
  });

  it("preserves a saved legacy batch size without allowing other unsupported choices", () => {
    const form = { ...defaultCascadePlatform(), catalogBatchSize: 100 };
    expect(validateCascadeStep(form, 4, 100)).toEqual([]);
    expect(validateCascadeStep({ ...form, catalogBatchSize: 64 }, 4, 100)).toEqual([]);
    expect(validateCascadeStep({ ...form, catalogBatchSize: 99 }, 4, 100)).not.toEqual([]);
    expect(validateCascadeStep({ ...form, catalogBatchSize: 0 }, 4, 0)).not.toEqual([]);
  });

  it("defaults to unlimited streams and accepts zero while rejecting negative limits", () => {
    const form = defaultCascadePlatform();
    expect(form.maxStreams).toBe(0);
    expect(validateCascadeStep({ ...form, maxStreams: 0 }, 4)).toEqual([]);
    expect(validateCascadeStep({ ...form, maxStreams: 4 }, 4)).toEqual([]);
    expect(validateCascadeStep({ ...form, maxStreams: -1 }, 4)).not.toEqual([]);
  });

  it.each(["192.168.10.220", " 192.0.2.1 ", "2001:db8::1", "::1", "::ffff:192.0.2.1"])(
    "accepts upstream IP address %s in field, step and save validation",
    host => {
      const form = { ...defaultCascadePlatform(), host };
      expect(cascadeFormFieldErrors(form, { host: true }).host).toBe("");
      expect(validateCascadeStep(form, 1)).not.toContain("请输入有效的 IPv4 或 IPv6 地址");
      expect(validateCascadePlatform(form)).not.toContain("请输入有效的 IPv4 或 IPv6 地址");
    }
  );

  it.each([
    "999.168.1.1",
    "192.168.1",
    "192.168.1.1.1",
    "01.2.3.4",
    "sip.example.com",
    "localhost",
    "http://192.0.2.1",
    "192.0.2.1:5060",
    "2001:db8:::1",
    "[2001:db8::1]"
  ])("rejects invalid upstream IP address %s at every validation entry point", host => {
    const form = { ...defaultCascadePlatform(), host };
    expect(cascadeFormFieldErrors(form, {}).host).toBe("");
    expect(cascadeFormFieldErrors(form, { host: true }).host).toBe("请输入有效的 IPv4 或 IPv6 地址");
    expect(validateCascadeStep(form, 1)).toContain("请输入有效的 IPv4 或 IPv6 地址");
    expect(validateCascadePlatform(form)).toContain("请输入有效的 IPv4 或 IPv6 地址");
  });

  it("reports an empty upstream address as required after blur", () => {
    expect(cascadeFormFieldErrors(defaultCascadePlatform(), { host: true }).host).toBe("上级地址不能为空");
  });

  it("defaults to the charset of the negotiated protocol without an explicit override", () => {
    expect(defaultCascadePlatform().charsetOverride).toBe("");
  });

  it("validates only the current step and enforces configured identity lengths", () => {
    const form = {
      ...defaultCascadePlatform(),
      name: "上级",
      upstreamServerId: "34020000002000000001",
      upstreamDomain: "3402000000",
      host: "192.0.2.1"
    };
    expect(validateCascadeStep(form, 1)).toEqual([]);
    expect(validateCascadeStep({ ...form, authUsername: "" }, 1)).toEqual([]);
    expect(validateCascadeStep({ ...form, authUsername: "34020000001320000001" }, 1)).toEqual([]);
    expect(validateCascadeStep(form, 2).length).toBeGreaterThan(0);
    expect(validateCascadeStep({ ...form, upstreamDomain: "123" }, 1)).toContain("上级域必须是 10 位数字");
    expect(validateCascadeStep({ ...form, authUsername: "user" }, 1)).toContain("认证用户名必须是 20 位数字或留空");
    expect(validateCascadeStep({ ...form, registerExpires: 86401 }, 3).length).toBeGreaterThan(0);
    expect(validateCascadeStep({ ...form, maxStreams: 10001 }, 4).length).toBeGreaterThan(0);
  });

  it.each([1, 60, 3599])("rejects a positive register expiry below the GB minimum: %s", registerExpires => {
    const form = { ...defaultCascadePlatform(), registerExpires };
    expect(validateCascadeStep(form, 3)).toContain("注册有效期或心跳间隔超出允许范围");
  });

  it("accepts exactly 3600 seconds as the minimum register expiry", () => {
    expect(validateCascadeStep({ ...defaultCascadePlatform(), registerExpires: 3600 }, 3)).toEqual([]);
  });

  it("maps runtime facts to product conclusions", () => {
    expect(
      cascadePresentation({ enabled: false, overall: "offline", registration: "unregistered", heartbeat: "unknown" })
    ).toMatchObject({ label: "已停用", color: "gray" });
    expect(
      cascadePresentation({ enabled: true, overall: "online", registration: "registered", heartbeat: "healthy" })
    ).toMatchObject({ label: "在线", color: "green" });
    expect(cascadePresentation({ enabled: true, overall: "offline", registration: "expired", heartbeat: "stale" })).toMatchObject(
      { label: "注册已过期", color: "red" }
    );
  });

  it("先算状态键、再查字典：六个分支的键与兜底文案一一对应", () => {
    expect(DICT_CODE_CASCADE_REGISTER_STATE).toBe("cascade_register_state");
    expect(CASCADE_REGISTER_STATE_LABEL_FALLBACK).toEqual({
      disabled: "已停用",
      online: "在线",
      registration_expired: "注册已过期",
      heartbeat_stale: "心跳超时",
      awaiting_heartbeat: "等待心跳",
      awaiting_registration: "等待注册"
    });

    const keyOf = (overall: string, registration: string, heartbeat: string) =>
      cascadeRegisterStateKey({
        enabled: true,
        overall: overall as never,
        registration: registration as never,
        heartbeat: heartbeat as never
      });
    expect(keyOf("online", "registered", "healthy")).toBe("online");
    expect(keyOf("offline", "expired", "stale")).toBe("registration_expired");
    expect(keyOf("offline", "registered", "stale")).toBe("heartbeat_stale");
    expect(keyOf("offline", "registered", "healthy")).toBe("awaiting_heartbeat");
    expect(keyOf("offline", "unregistered", "unknown")).toBe("awaiting_registration");
  });

  it("字典改名后界面跟着变，但**色与说明不受影响**（它们不是字典值域）", () => {
    const platform = { enabled: true, overall: "offline", registration: "expired", heartbeat: "stale" } as const;
    expect(cascadePresentation(platform, { registration_expired: "注册失效" })).toEqual({
      label: "注册失效",
      color: "red",
      detail: "上级平台未保持注册"
    });
    // 字典没给的键回落兜底，不塌成 key
    expect(cascadePresentation(platform, {})).toMatchObject({ label: "注册已过期", color: "red" });
  });

  it("rejects invalid identities and ports without leaking backend diagnostics", () => {
    const errors = validateCascadePlatform({
      ...defaultCascadePlatform(),
      name: "",
      upstreamServerId: "1",
      host: "bad host",
      port: 0,
      localDeviceId: "2",
      localDomain: "",
      localSipIp: "not an ip",
      localSipPort: 70000
    });
    expect(errors).toEqual(
      expect.arrayContaining([
        "平台名称不能为空",
        "上级平台 ID 必须是 20 位数字",
        "上级端口必须在 1-65535 之间",
        "本平台设备 ID 必须是 20 位数字"
      ])
    );
    expect(errors.length).toBeGreaterThan(4);
  });

  it("maps the configured local SIP identity into a new cascade platform", () => {
    expect(
      cascadeLocalIdentityDefaults({
        listenIp: "0.0.0.0",
        advertiseIp: "192.168.10.106",
        port: 5061,
        domain: "3402000000",
        serverId: "34020000001320000001"
      })
    ).toEqual({
      localDeviceId: "34020000001320000001",
      localDomain: "3402000000",
      localSipIp: "192.168.10.106",
      localSipPort: 5061,
      mediaAdvertiseIp: "192.168.10.106",
      authUsername: "34020000001320000001"
    });
  });

  it("does not use a wildcard listener as the local advertised address", () => {
    expect(
      cascadeLocalIdentityDefaults({
        listenIp: "0.0.0.0",
        advertiseIp: "",
        port: 5060,
        domain: "3402000000",
        serverId: "34020000001320000001"
      }).localSipIp
    ).toBe("");
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
    const form = {
      ...defaultCascadePlatform(),
      name: "上级",
      upstreamServerId: "34020000002000000001",
      localDeviceId: "34020000001320000001",
      mediaAdvertiseIp: "bad ip"
    };
    const touched = { name: true, upstreamServerId: true, localDeviceId: true, mediaAdvertiseIp: true };
    const errors = cascadeFormFieldErrors(form, touched);
    expect(errors.name).toBe("");
    expect(errors.upstreamServerId).toBe("");
    expect(errors.localDeviceId).toBe("");
    expect(errors.mediaAdvertiseIp).toBe("地址格式不正确");
  });

  // 保存共享前必须先收敛「勾选通道 → 所属设备」：解析是异步的，用户可能在请求
  // 回来前就点保存，此前会把"还没解析完"误判成"部分通道无法关联所属设备"。
  it("lists selected channels that still have no resolved source device", () => {
    const resolved = new Map<number, number>([
      [3537, 5147],
      [3539, 5147]
    ]);
    expect(pendingSourceChannelIds([3537, 3538, 3539], resolved)).toEqual([3538]);
    expect(pendingSourceChannelIds([3537, 3539], resolved)).toEqual([]);
    expect(pendingSourceChannelIds([], resolved)).toEqual([]);
  });

  it("treats a not-yet-resolved selection as pending, not as unresolvable", () => {
    const stillResolving = new Map<number, number>([[3537, 0]]);
    expect(pendingSourceChannelIds([3537], stillResolving)).toEqual([3537]);
  });

  it("names the offending channel by its GB code, falling back to the local id", () => {
    expect(channelSourceLabel({ channelId: "34020000001320000020" }, 3538)).toBe("34020000001320000020");
    expect(channelSourceLabel({ publishedChannelId: "34020000001320000010" }, 3539)).toBe("34020000001320000010");
    expect(channelSourceLabel({ channelId: "  " }, 3538)).toBe("#3538");
    expect(channelSourceLabel(undefined, 3538)).toBe("#3538");
  });
});
