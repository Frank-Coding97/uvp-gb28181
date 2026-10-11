import { describe, expect, it } from "vitest";
import type { SipNetworkAddress } from "@/api/gb28181";
import {
  isConcreteIp,
  isVirtualListenIp,
  mediaAddressesCanContinue,
  resolveMediaAddressPolicy,
  validateHookIp,
  validateOptionalHookIp,
  validateOptionalSdpIp,
  validateOptionalStreamIp,
  validateSdpIp,
  validateStreamIp,
  type MediaAddressRule
} from "./mediaNetworkAddress";

/** 只校验格式、不强制必填（节点级「覆盖」字段的口径：留空 = 继承平台默认）。 */
const formatOnly: MediaAddressRule = { optional: true, fallback: "" };

describe("optional media network addresses", () => {
  it("accepts empty values and concrete IPv4 or IPv6 addresses", () => {
    expect(validateOptionalHookIp("")).toBe("");
    expect(validateOptionalHookIp("192.0.2.12")).toBe("");
    expect(validateOptionalHookIp("2001:db8::12")).toBe("");
    expect(validateOptionalStreamIp("")).toBe("");
    expect(validateOptionalStreamIp("media.example.com")).toBe("");
    expect(validateOptionalStreamIp("192.0.2.12")).toBe("");
    expect(validateOptionalStreamIp("2001:db8::12")).toBe("");
  });

  it("rejects wildcard, malformed, and non-host URL values", () => {
    expect(isConcreteIp("0.0.0.0")).toBe(false);
    expect(isConcreteIp("::")).toBe(false);
    expect(isConcreteIp("239.1.2.3")).toBe(false);
    expect(isConcreteIp("ff02::1")).toBe(false);
    expect(validateOptionalHookIp("https://media.example.com")).not.toBe("");
    expect(validateOptionalStreamIp("https://media.example.com/live")).not.toBe("");
    expect(validateOptionalStreamIp("media.example.com:8080")).not.toBe("");
    expect(validateOptionalStreamIp("123")).not.toBe("");
    expect(validateOptionalStreamIp("256.0.2.1")).not.toBe("");
    expect(validateOptionalStreamIp("2001:db8:::12")).not.toBe("");
  });

  // 媒体节点的「覆盖」字段用它：留空 = 继承平台默认，所以空值必须放行；
  // 但填了回环地址仍要拦（那会让该节点的点播永远没有画面）。
  it("lets the node-level SDP override stay empty but still checks a filled value", () => {
    expect(validateOptionalSdpIp("")).toBe("");
    expect(validateOptionalSdpIp("   ")).toBe("");
    expect(validateOptionalSdpIp("192.0.2.12")).toBe("");
    expect(validateOptionalSdpIp("media.example.com")).toBe("");
    expect(validateOptionalSdpIp("127.0.0.1")).not.toBe("");
    expect(validateOptionalSdpIp("polaris-media")).not.toBe("");
  });
});

// SDP IP 决定设备把 RTP 推向哪里。留空是否安全取决于「能否推断出兜底地址」，
// 但**填了值就必须可用** —— 回环地址的故障表征是「信令全成功但没有画面」，
// 现场看不出是配置问题，所以必须当场拦。
describe("SDP IP format rules", () => {
  it("accepts concrete addresses and resolvable domains", () => {
    expect(validateSdpIp("192.0.2.12", formatOnly)).toBe("");
    expect(validateSdpIp("2001:db8::12", formatOnly)).toBe("");
    expect(validateSdpIp("media.example.com", formatOnly)).toBe("");
  });

  it("rejects loopback and unspecified addresses", () => {
    // 这两个正是线上「注册成功但拉不到流」的真凶。
    expect(validateSdpIp("127.0.0.1", formatOnly)).not.toBe("");
    expect(validateSdpIp("127.0.0.53", formatOnly)).not.toBe("");
    expect(validateSdpIp("::1", formatOnly)).not.toBe("");
    expect(validateSdpIp("0.0.0.0", formatOnly)).not.toBe("");
    expect(validateSdpIp("::", formatOnly)).not.toBe("");
  });

  it("rejects single-label hostnames such as container names", () => {
    // wvp 的 sdp-ip 兜底会退回 media.ip，Docker 下正是容器名，设备无法解析。
    expect(validateSdpIp("polaris-media", formatOnly)).not.toBe("");
    expect(validateSdpIp("localhost", formatOnly)).not.toBe("");
  });

  it("rejects values carrying scheme, port or path", () => {
    expect(validateSdpIp("https://media.example.com/live", formatOnly)).not.toBe("");
    expect(validateSdpIp("media.example.com:8080", formatOnly)).not.toBe("");
    expect(validateSdpIp("256.0.2.1", formatOnly)).not.toBe("");
  });
});

function nic(overrides: Partial<SipNetworkAddress>): SipNetworkAddress {
  return {
    ip: "192.168.1.10",
    cidr: "192.168.1.10/24",
    loopback: false,
    virtual: false,
    recommended: false,
    more: true,
    listenOnly: false,
    ...overrides
  };
}

const physicalLan = { deploymentMode: "lan" as const, listenIp: "192.168.1.10", advertiseIp: "192.168.1.10" };
const wildcardLan = { deploymentMode: "lan" as const, listenIp: "0.0.0.0", advertiseIp: "" };

describe("resolveMediaAddressPolicy", () => {
  // 规则①：真实网卡 ⇒ 三项可留空，留空跟随该网卡。
  it("lets all three fields fall back to a concrete physical NIC", () => {
    const policy = resolveMediaAddressPolicy({ ...physicalLan, networkItems: [nic({})] });
    for (const rule of [policy.hookIp, policy.sdpIp, policy.streamIp]) {
      expect(rule.optional).toBe(true);
      expect(rule.fallback).toBe("192.168.1.10");
    }
  });

  // 规则②：虚拟网卡地址设备侧不可达，托底等于埋雷 ⇒ 三项必填。
  it("requires all three fields when the selected NIC is virtual", () => {
    const policy = resolveMediaAddressPolicy({
      ...physicalLan,
      networkItems: [nic({ interfaceName: "VMware Network Adapter VMnet1", virtual: true })]
    });
    for (const rule of [policy.hookIp, policy.sdpIp, policy.streamIp]) {
      expect(rule.optional).toBe(false);
      expect(rule.fallback).toBe("");
    }
  });

  // 规则②：0.0.0.0 推断不出唯一地址 ⇒ 三项必填。
  it("requires all three fields for the wildcard listen address", () => {
    const policy = resolveMediaAddressPolicy({ ...wildcardLan, networkItems: [] });
    for (const rule of [policy.hookIp, policy.sdpIp, policy.streamIp]) {
      expect(rule.optional).toBe(false);
    }
  });

  // 规则③：公网模式下设备/浏览器在公网侧，只有对外地址可达；
  //        Hook 是 ZLM 回调平台，不出本机，仍走监听网卡。
  it("splits public mode between the advertise IP and the listen NIC", () => {
    const policy = resolveMediaAddressPolicy({
      deploymentMode: "public",
      listenIp: "10.0.8.3",
      advertiseIp: "203.0.113.10",
      networkItems: [nic({ ip: "10.0.8.3" })]
    });
    expect(policy.hookIp).toEqual({ optional: true, fallback: "10.0.8.3" });
    expect(policy.sdpIp).toEqual({ optional: true, fallback: "203.0.113.10" });
    expect(policy.streamIp).toEqual({ optional: true, fallback: "203.0.113.10" });
  });

  // 公网模式下 SDP/Stream 与网卡无关，所以虚拟网卡只影响 Hook。
  it("only requires Hook IP in public mode with a virtual NIC", () => {
    const policy = resolveMediaAddressPolicy({
      deploymentMode: "public",
      listenIp: "192.168.56.1",
      advertiseIp: "203.0.113.10",
      networkItems: [nic({ ip: "192.168.56.1", interfaceName: "VirtualBox Host-Only Network", virtual: true })]
    });
    expect(policy.hookIp.optional).toBe(false);
    expect(policy.sdpIp.optional).toBe(true);
    expect(policy.streamIp.optional).toBe(true);
  });

  it("falls back to required while the public IP is still empty", () => {
    const policy = resolveMediaAddressPolicy({ deploymentMode: "public", listenIp: "10.0.8.3", advertiseIp: "" });
    expect(policy.sdpIp.optional).toBe(false);
    expect(policy.streamIp.optional).toBe(false);
  });

  it("treats a missing network list as non-virtual", () => {
    expect(isVirtualListenIp("192.168.1.10")).toBe(false);
    expect(resolveMediaAddressPolicy(physicalLan).hookIp.optional).toBe(true);
  });
});

describe("required media addresses", () => {
  const required = { optional: false, fallback: "" };

  it("blocks empty values when no fallback can be inferred", () => {
    expect(validateHookIp("", required)).not.toBe("");
    expect(validateSdpIp("", required)).not.toBe("");
    expect(validateStreamIp("", required)).not.toBe("");
    expect(validateSdpIp("   ", required)).not.toBe("");
  });

  it("still validates the format of a filled value", () => {
    expect(validateHookIp("192.168.1.10", required)).toBe("");
    expect(validateHookIp("0.0.0.0", required)).not.toBe("");
    expect(validateStreamIp("https://media.example.com", required)).not.toBe("");
  });

  it("accepts empty values when a fallback exists", () => {
    const optional = { optional: true, fallback: "192.168.1.10" };
    expect(validateHookIp("", optional)).toBe("");
    expect(validateSdpIp("", optional)).toBe("");
    expect(validateStreamIp("", optional)).toBe("");
  });
});

describe("mediaAddressesCanContinue", () => {
  const required = { optional: false, fallback: "" };
  const optional = (fallback: string) => ({ optional: true, fallback });
  const requiredPolicy = { hookIp: required, sdpIp: required, streamIp: required };

  it("blocks continuation while a required address is missing", () => {
    expect(mediaAddressesCanContinue(requiredPolicy, "", "", "")).toBe(false);
    expect(mediaAddressesCanContinue(requiredPolicy, "192.0.2.12", "", "")).toBe(false);
    expect(mediaAddressesCanContinue(requiredPolicy, "192.0.2.12", "192.0.2.12", "media.example.com")).toBe(true);
  });

  it("allows blank values when the policy infers them", () => {
    const policy = { hookIp: optional("192.168.1.10"), sdpIp: optional("192.168.1.10"), streamIp: optional("192.168.1.10") };
    expect(mediaAddressesCanContinue(policy, "", "", "")).toBe(true);
    // 填了值仍要过格式关。
    expect(mediaAddressesCanContinue(policy, "0.0.0.0", "", "")).toBe(false);
    expect(mediaAddressesCanContinue(policy, "", "polaris-media", "")).toBe(false);
    expect(mediaAddressesCanContinue(policy, "", "", "media.example.com:8080")).toBe(false);
  });
});
