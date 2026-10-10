import { describe, expect, it } from "vitest";
import {
  buildConfigUpdatePayload,
  defaultLogCleanupConfig,
  isLogCleanupConfigValid,
  logCleanupPageRoute
} from "./sysconfigState";

const regularConfig = {
  system: { systemLogo: "logo.svg" },
  safe: { minPasswordLength: 8 },
  captcha: { open: true, length: 6 }
} as any;

describe("日志清理配置状态", () => {
  it("提供确认过的默认保留期和服务端托管标记", () => {
    expect(defaultLogCleanupConfig).toEqual({
      sipRetentionDays: 7,
      operationRetentionDays: 180,
      loginRetentionDays: 180,
      jobRetentionDays: 30,
      playbackRetentionDays: 7,
      schedulerRetentionDays: 7,
      configured: false
    });
  });

  it("使用已存在的系统配置路径并通过 tab 查询参数打开日志清理页签", () => {
    expect(logCleanupPageRoute).toEqual({ path: "/system/sysconfig", query: { tab: "logCleanup" } });
  });

  it.each([1, 365])("允许 %i 天边界值", days => {
    const config = { ...defaultLogCleanupConfig, operationRetentionDays: days };
    expect(isLogCleanupConfigValid(config)).toBe(true);
  });

  it.each([null, 0, -1, 1.5, 366])("拒绝非法保留期 %s", days => {
    const config = { ...defaultLogCleanupConfig, operationRetentionDays: days };
    expect(isLogCleanupConfigValid(config)).toBe(false);
  });

  it("日志清理页签只提交六个天数字段，不允许客户端设置 configured", () => {
    const payload = buildConfigUpdatePayload("logCleanup", regularConfig, defaultLogCleanupConfig);

    expect(payload).toEqual({
      logCleanup: {
        sipRetentionDays: 7,
        operationRetentionDays: 180,
        loginRetentionDays: 180,
        jobRetentionDays: 30,
        playbackRetentionDays: 7,
        schedulerRetentionDays: 7
      }
    });
  });

  it("普通系统配置页签保留原请求且不携带日志清理配置", () => {
    expect(buildConfigUpdatePayload("server", regularConfig, defaultLogCleanupConfig)).toEqual(regularConfig);
  });
});
