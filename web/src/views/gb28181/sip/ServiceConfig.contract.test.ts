import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/ServiceConfig.vue"), "utf8");

describe("SIP 服务配置中的日志保留期入口", () => {
  it("保留采集开关，保留期入口已下线（唯一入口在系统配置日志清理页签）", () => {
    expect(source).toContain('field="sipLogEnabled"');
    expect(source).not.toContain('label="SIP 日志保留天数"');
    expect(source).not.toContain("logCleanupPageRoute");
    expect(source).not.toContain("sipLogRetentionDays");
  });

  it("聚合保存不再携带 SIP 保留期，避免覆盖日志清理页的配置", () => {
    const saveBody = source.slice(
      source.indexOf("async function saveConfig()"),
      source.indexOf("async function loadServiceConfig()")
    );
    expect(saveBody).toContain("sipLog: {");
    expect(saveBody).not.toContain("retentionDays: latestSipLogConfig");
    expect(saveBody).not.toContain("await fetchServiceConfig()");
  });

  it("聚合保存失败时显示后端原始业务消息", () => {
    expect(source).toContain('Message.error(error?.message || "保存国标服务配置失败")');
  });
});
