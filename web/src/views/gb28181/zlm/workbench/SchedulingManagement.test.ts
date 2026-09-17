import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("SchedulingManagement", () => {
  it("exposes scheduling logs as a standalone menu page", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/SchedulingManagement.vue"), "utf8");
    expect(source).toContain("SchedulerLogPanel");
    expect(source).toMatch(/#logs=\"\{\s*active\s*\}\"/);
    expect(source).toContain(":active=\"active\"");
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).not.toContain("SchedulerStrategyPanel");
    expect(source).not.toContain('key: "strategy"');
  });
});
