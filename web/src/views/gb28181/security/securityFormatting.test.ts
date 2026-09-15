import { describe, expect, it } from "vitest";
import { formatAutomaticBanTTL, formatRemaining, formatSecurityAction, formatSecurityReason, formatShortWindowInviteRule, isHighRiskSecurityReason } from "./securityFormatting";
import { buildAccessRuleTogglePayload } from "./securityRules";

describe("security formatting", () => {
  it("formats Go duration nanoseconds as a remaining minute", () => {
    const now = Date.parse("2026-08-18T09:00:00Z");
    expect(formatRemaining("2026-08-18T09:00:00Z", 60_000_000_000, now)).toBe("剩余 1 分钟");
  });

  it("formats escalating finite automatic ban steps", () => {
    expect(formatAutomaticBanTTL([
      { score: 100, ttl: 60 },
      { score: 200, ttl: 600 },
      { score: 500, ttl: 3600 }
    ])).toBe("1 分钟起，最高 1 小时");
  });

  it("formats permanent bans only when the decision marks them permanent", () => {
    const now = Date.parse("2026-08-18T09:00:00Z");
    expect(formatRemaining("2026-08-18T09:00:00Z", 0, now, true)).toBe("永久封禁");
    expect(formatRemaining("2026-08-18T09:00:00Z", 0, now, false)).toBe("未设置到期时间");
  });

  it("recognizes the permanent policy contract only at the current ban score", () => {
    expect(formatAutomaticBanTTL([{ score: 250, ttl: 0 }], true, 250)).toBe("永久封禁（需人工解封）");
    expect(formatAutomaticBanTTL([{ score: 250, ttl: 0 }], true, 100)).toBe("策略读取中");
    expect(formatAutomaticBanTTL([{ score: 250, ttl: 0 }], false, 250)).toBe("策略读取中");
  });

  it("maps the persistent unauthorized INVITE reason and keeps unknown reasons visible", () => {
    expect(formatSecurityReason("unauthorized_invite_accumulation")).toBe("10 分钟累计 10 次未授权 INVITE");
    expect(formatSecurityReason("future_security_reason")).toBe("future_security_reason");
  });

  it("explains registration configuration failures and ID enumeration", () => {
    expect(formatSecurityReason("digest_failure")).toBe("密码或鉴权配置错误，请检查配置后重新注册");
    expect(formatSecurityReason("register_id_invalid")).toBe("设备 ID 非 20 位数字，请检查配置后重新注册");
    expect(formatSecurityReason("register_id_enumeration")).toBe("10 分钟内至少 10 个不同 REGISTER 事务，枚举至少 3 个不同非法设备 ID");
  });

  it("keeps high-risk enumeration separate from the normal drop action", () => {
    expect(isHighRiskSecurityReason("register_id_enumeration")).toBe(true);
    expect(isHighRiskSecurityReason("register_id_invalid")).toBe(false);
    expect(formatSecurityAction("drop")).toBe("已拒绝");
    expect(formatSecurityAction("ban")).toBe("已拒绝并封禁");
  });

  it("formats the short-window INVITE count from the live policy score", () => {
    expect(formatShortWindowInviteRule(10, 120)).toBe("10 秒内 6 次未授权 INVITE");
    expect(formatShortWindowInviteRule(600, 60)).toBe("600 秒内 3 次未授权 INVITE");
  });

  it("builds the toggle payload with the original expiry and unchanged rule fields", () => {
    const payload = buildAccessRuleTogglePayload({
      listType: "blacklist",
      matchType: "ip",
      value: "203.0.113.12",
      scope: "all_sip",
      note: "临时来源",
      expiresAt: "2026-08-18T10:00:00.000Z"
    }, false);

    expect(payload).toEqual({
      listType: "blacklist",
      matchType: "ip",
      matchValue: "203.0.113.12",
      scope: "all_sip",
      status: "disabled",
      expiresAt: "2026-08-18T10:00:00.000Z",
      note: "临时来源"
    });

    const permanentPayload = buildAccessRuleTogglePayload({
      listType: "allowlist",
      matchType: "cidr",
      value: "198.51.100.0/24",
      scope: "all_sip",
      note: "可信出口",
      expiresAt: undefined
    }, true);

    expect(permanentPayload).toHaveProperty("expiresAt", undefined);
  });
});
