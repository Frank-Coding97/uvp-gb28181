import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("scheduler strategy effective boundary", () => {
  it("states that a switch only affects newly allocated invites", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8");
    const legacyShell = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerStrategy.vue"), "utf8");
    expect(source).toContain("只影响新点播");
    // effectiveFrom 的「生效范围」区块在 833798e9「调度策略视图下线」中被有意移除，
    // 语义改由「只影响新点播」一句承担；同目录 SchedulerStrategyPanel.test.ts 已同步为
    // not.toContain，这条旧位置守卫当时漏改，这里对齐。
    // 注意后端 toggleScheduler 应答仍返回 effectiveFrom（api/gb28181-zlm.ts 的类型保留），
    // 只是前端不再展示 —— 所以守卫的是「不许把这块 UI 加回来」，不是「字段不存在」。
    expect(source).not.toContain("effectiveFrom");
    expect(source).not.toContain("保存后立即生效,无需重启");
    expect(source).toContain("gb28181:zlm:scheduler:manage");
    expect(source).toContain("当前页面为只读");
    expect(legacyShell).toContain("SchedulerStrategyPanel");
  });
});
