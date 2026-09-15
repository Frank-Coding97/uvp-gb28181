import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("scheduler strategy effective boundary", () => {
  it("states that a switch only affects newly allocated invites", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8");
    const legacyShell = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SchedulerStrategy.vue"), "utf8");
    expect(source).toContain("只影响新点播");
    expect(source).toContain("effectiveFrom");
    expect(source).not.toContain("保存后立即生效,无需重启");
    expect(source).toContain("gb28181:zlm:scheduler:manage");
    expect(source).toContain("当前页面为只读");
    expect(legacyShell).toContain("SchedulerStrategyPanel");
  });
});
