import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/sysjobresults/sysjobresultslist.vue"), "utf8");

describe("定时任务执行结果摘要", () => {
  it("新增执行摘要列，普通任务没有摘要时显示占位符", () => {
    expect(source).toContain('title="执行摘要"');
    expect(source).toContain('record.summary?.trim() || "—"');
  });
});

describe("定时任务页面外壳", () => {
  /**
   * ⛔ 不能再用 `<a-card>` 包页面：Arco 卡片的暗色背景是**中性灰 `--color-bg-2`（#171719）**，
   * 而平台外壳是**蓝黑 `--uvp-shell-bg`（#101823）**。两者同屏时卡片区发灰、与搜索面板/表格
   * 对不上色，看起来像"背景不对"（实测分页条那一片就是 #171719）。
   * 基准页（sysapi / dictionary / log 等）一律是 snow-inner + uvp-page-shell-flat 直出。
   */
  it("不再套 Arco 卡片，避免暗色下露出中性灰底", () => {
    expect(source).not.toMatch(/<a-card[\s>]/);
  });

  it("操作列按钮采用「图标 + 文字」形式", () => {
    const actionCell = source.slice(source.indexOf('title="操作"'));
    expect(actionCell).toContain("<icon-delete />");
    expect(actionCell).toContain("<span>删除</span>");
  });
});
