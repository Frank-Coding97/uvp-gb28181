import { describe, expect, it } from "vitest";
import {
  isConcreteIp,
  mediaNetworkAddressesCanContinue,
  validateOptionalHookIp,
  validateOptionalStreamIp
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

  it("gates continuation only when a populated media address is invalid", () => {
    expect(mediaNetworkAddressesCanContinue("", "")).toBe(true);
    expect(mediaNetworkAddressesCanContinue("192.0.2.12", "media.example.com")).toBe(true);
    expect(mediaNetworkAddressesCanContinue("0.0.0.0", "")).toBe(false);
    expect(mediaNetworkAddressesCanContinue("", "media.example.com:8080")).toBe(false);
  });
});
