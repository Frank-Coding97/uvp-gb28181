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
