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

  // ⛔ 页面上「写着 7 天」不等于策略在跑：服务端 log_cleanup.configured 缺席时
  //   操作日志与定时任务日志一条都不删（线上曾留 151 万行撑爆磁盘）。
  //   两种状态都必须在页签上显性提示，不能只靠天数输入框。
  it("区分并显性提示清理策略已启用 / 未启用", () => {
    expect(sysconfigSource).toContain("isLogCleanupEnabled");
    expect(sysconfigSource).toContain("清理策略未启用");
    expect(sysconfigSource).toContain("清理策略已启用");
  });
});
