import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/ServiceConfig.vue"), "utf8");

describe("SIP 服务配置中的日志保留期入口", () => {
  it("保留采集开关，将保留期设为只读并链接到系统配置日志清理页签", () => {
    expect(source).toContain('field="sipLogEnabled"');
    expect(source).toContain('label="SIP 日志保留天数"');
    expect(source).not.toContain('ref="sipLogRetentionField"');
    expect(source).toContain("logCleanupPageRoute");
  });

  it("聚合配置保存前刷新 SIP 保留期，避免覆盖统一配置中的最新值", () => {
    const saveBody = source.slice(
      source.indexOf("async function saveConfig()"),
      source.indexOf("async function loadServiceConfig()")
    );
    expect(saveBody.indexOf("await fetchServiceConfig()")).toBeGreaterThan(-1);
    expect(saveBody.indexOf("await fetchServiceConfig()")).toBeLessThan(saveBody.indexOf("await updateServiceConfig({"));
    expect(saveBody).toContain("retentionDays: latestSipLogConfig.data.sipLog.retentionDays");
  });

  it("聚合保存失败时显示后端原始业务消息", () => {
    expect(source).toContain('Message.error(error?.message || "保存国标服务配置失败")');
  });
});
