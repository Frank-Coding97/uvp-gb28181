import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sysconfigSource = readFileSync(resolve(process.cwd(), "src/views/system/sysconfig/sysconfig.vue"), "utf8");

describe("系统配置日志清理页签", () => {
  it("提供六类保留天数输入并从真实系统配置路由定位到该页签", () => {
    expect(sysconfigSource).toContain('key="logCleanup" title="日志清理"');
    expect(sysconfigSource).toContain("route.query.tab === logCleanupPageRoute.query.tab");
    expect(sysconfigSource).toContain("logCleanupPageRoute");
    expect(sysconfigSource.match(/key: "\w+RetentionDays"/g)).toHaveLength(6);
    expect(sysconfigSource).toContain(':min="1"');
    expect(sysconfigSource).toContain(':max="365"');
  });

  it("提交失败时展示后端原因，不会用成功提示覆盖热应用错误", () => {
    expect(sysconfigSource).toContain('(error as Error)?.message || "保存配置失败"');
  });
});
