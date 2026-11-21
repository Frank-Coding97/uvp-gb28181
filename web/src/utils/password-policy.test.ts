import { describe, expect, it } from "vitest";
import {
  PASSWORD_MIN_LENGTH_FALLBACK,
  PASSWORD_SPECIAL_CHARS,
  buildPasswordPolicyRules,
  firstPasswordPolicyError,
  passwordMinLength,
  passwordPolicySatisfied
} from "./password-policy";

describe("password policy", () => {
  it("falls back to the default minimum length when the config is missing", () => {
    expect(PASSWORD_MIN_LENGTH_FALLBACK).toBe(8);
    expect(passwordMinLength()).toBe(8);
    expect(passwordMinLength({ minLength: 0 })).toBe(8);
    expect(passwordMinLength({ minLength: Number.NaN })).toBe(8);
    expect(passwordMinLength({ minLength: 6 })).toBe(6);
  });

  it("keeps the special character set in step with the backend regex", () => {
    // 后端 password.go 用 /[!@#$%]/，此处必须一模一样
    expect(PASSWORD_SPECIAL_CHARS).toBe("!@#$%");
  });

  it("exposes one rule per policy item when special characters are required", () => {
    const rules = buildPasswordPolicyRules("", { minLength: 8, requireSpecialChar: true });
    expect(rules.map(rule => rule.key)).toEqual(["length", "case", "special"]);
    expect(rules.map(rule => rule.label)).toEqual(["最少 8 位", "包含大小写字母", "包含特殊字符 !@#$%"]);
    expect(rules.every(rule => !rule.satisfied)).toBe(true);
  });

  it("drops the special character rule when the security switch is off", () => {
    const rules = buildPasswordPolicyRules("Abcd1234", { minLength: 8, requireSpecialChar: false });
    expect(rules.map(rule => rule.key)).toEqual(["length", "case"]);
    expect(passwordPolicySatisfied("Abcd1234", { minLength: 8, requireSpecialChar: false })).toBe(true);
  });

  it("requires length, upper+lower case and a special character", () => {
    const options = { minLength: 8, requireSpecialChar: true };
    expect(passwordPolicySatisfied("Abcdefg!", options)).toBe(true);
    // 只缺一项也不放行
    expect(passwordPolicySatisfied("abcdefg!", options)).toBe(false);
    expect(passwordPolicySatisfied("ABCDEFG!", options)).toBe(false);
    expect(passwordPolicySatisfied("Abcdefgh", options)).toBe(false);
    expect(passwordPolicySatisfied("Abcdefg", options)).toBe(false);
  });

  it("reports the first unmet rule so the message matches the backend one", () => {
    const options = { minLength: 8, requireSpecialChar: true };
    expect(firstPasswordPolicyError("Abc1", options)).toBe("密码长度不能少于8位");
    expect(firstPasswordPolicyError("abcdefg!", options)).toBe("密码必须同时包含大写和小写字母");
    expect(firstPasswordPolicyError("Abcdefgh", options)).toBe("密码必须包含特殊字符 !@#$%");
    expect(firstPasswordPolicyError("Abcdefg!", options)).toBe("");
  });
});
