import { describe, expect, it } from "vitest";
import {
  isConcreteIp,
  mediaNetworkAddressesCanContinue,
  validateOptionalHookIp,
  validateOptionalStreamIp,
  validateRequiredSdpIp
} from "./mediaNetworkAddress";

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
});

// SDP IP 是必填项：它决定设备把RTP 推向哪里。留空或填成回环地址时，
// 故障表征是「信令全成功但没有画面」，现场看不出是配置问题 —— 所以必须当场拦。
describe("required SDP IP", () => {
  it("requires a value", () => {
    expect(validateRequiredSdpIp("")).not.toBe("");
    expect(validateRequiredSdpIp("   ")).not.toBe("");
  });

  it("accepts concrete addresses and resolvable domains", () => {
    expect(validateRequiredSdpIp("192.0.2.12")).toBe("");
    expect(validateRequiredSdpIp("2001:db8::12")).toBe("");
    expect(validateRequiredSdpIp("media.example.com")).toBe("");
  });

  it("rejects loopback and unspecified addresses", () => {
    // 这两个正是线上「注册成功但拉不到流」的真凶。
    expect(validateRequiredSdpIp("127.0.0.1")).not.toBe("");
    expect(validateRequiredSdpIp("127.0.0.53")).not.toBe("");
    expect(validateRequiredSdpIp("::1")).not.toBe("");
    expect(validateRequiredSdpIp("0.0.0.0")).not.toBe("");
    expect(validateRequiredSdpIp("::")).not.toBe("");
  });

  it("rejects single-label hostnames such as container names", () => {
    // wvp 的 sdp-ip 兜底会退回 media.ip，Docker 下正是容器名，设备无法解析。
    expect(validateRequiredSdpIp("polaris-media")).not.toBe("");
    expect(validateRequiredSdpIp("localhost")).not.toBe("");
  });

  it("rejects values carrying scheme, port or path", () => {
    expect(validateRequiredSdpIp("https://media.example.com/live")).not.toBe("");
    expect(validateRequiredSdpIp("media.example.com:8080")).not.toBe("");
    expect(validateRequiredSdpIp("256.0.2.1")).not.toBe("");
  });
});

describe("mediaNetworkAddressesCanContinue", () => {
  it("blocks continuation while the required SDP IP is missing or unusable", () => {
    expect(mediaNetworkAddressesCanContinue("", "", "")).toBe(false);
    expect(mediaNetworkAddressesCanContinue("", "127.0.0.1", "")).toBe(false);
    expect(mediaNetworkAddressesCanContinue("", "polaris-media", "")).toBe(false);
    expect(mediaNetworkAddressesCanContinue("", "192.0.2.12", "")).toBe(true);
    expect(mediaNetworkAddressesCanContinue("", "192.0.2.12", "media.example.com")).toBe(true);
  });

  it("still gates the optional addresses independently", () => {
    expect(mediaNetworkAddressesCanContinue("0.0.0.0", "192.0.2.12", "")).toBe(false);
    expect(mediaNetworkAddressesCanContinue("", "192.0.2.12", "media.example.com:8080")).toBe(false);
  });
});
