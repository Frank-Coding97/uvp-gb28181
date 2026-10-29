import { describe, expect, it } from "vitest";
import {
  LOGIN_FAILURE_REASON_LABEL_FALLBACK,
  loginFailureReasonLabelFrom,
  loginFailureReasonLabelsFrom,
  loginFailureReasonOptionsFromLabels
} from "./loginFailureReason";

describe("登录失败原因", () => {
  it("兜底值域逐条锁定 —— 这 7 个 key 是后端白名单（sysloginlogparam.go）", () => {
    expect(LOGIN_FAILURE_REASON_LABEL_FALLBACK).toEqual({
      captcha_invalid: "验证码错误",
      user_not_found: "用户不存在",
      user_disabled: "用户未启用",
      account_locked: "账户已锁定",
      password_incorrect: "密码错误",
      session_create_failed: "会话创建失败",
      server_error: "服务器错误"
    });
  });

  it("字典为空 → 全用兜底，且顺序与兜底一致", () => {
    const labels = loginFailureReasonLabelsFrom([]);
    expect(loginFailureReasonOptionsFromLabels(labels).map(o => o.value)).toEqual(
      Object.keys(LOGIN_FAILURE_REASON_LABEL_FALLBACK)
    );
  });

  it("字典可以改展示名", () => {
    const labels = loginFailureReasonLabelsFrom([{ value: "server_error", name: "后端炸了", status: 1 }]);
    expect(loginFailureReasonLabelFrom(labels, "server_error")).toBe("后端炸了");
  });

  it("⛔ 白名单：字典里多出来的 key 必须被丢弃（不许新增后端不认的失败原因）", () => {
    const labels = loginFailureReasonLabelsFrom([{ value: "made_up_reason", name: "字典瞎加的原因", status: 1 }]);
    expect(labels["made_up_reason"]).toBeUndefined();
    expect(loginFailureReasonOptionsFromLabels(labels).map(o => o.value)).toEqual(
      Object.keys(LOGIN_FAILURE_REASON_LABEL_FALLBACK)
    );
  });

  it("停用项（status=0）不生效", () => {
    const labels = loginFailureReasonLabelsFrom([{ value: "server_error", name: "被停用的名字", status: 0 }]);
    expect(loginFailureReasonLabelFrom(labels, "server_error")).toBe("服务器错误");
  });

  it('未命中回「其他失败」，空值回 "-"（与改动前逐字一致）', () => {
    const labels = loginFailureReasonLabelsFrom([]);
    expect(loginFailureReasonLabelFrom(labels, "something_new")).toBe("其他失败");
    expect(loginFailureReasonLabelFrom(labels, "")).toBe("-");
    expect(loginFailureReasonLabelFrom(labels, undefined)).toBe("-");
    expect(loginFailureReasonLabelFrom(labels, null)).toBe("-");
  });
});
