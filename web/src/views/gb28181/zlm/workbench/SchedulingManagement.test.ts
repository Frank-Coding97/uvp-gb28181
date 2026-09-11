import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("SchedulingManagement", () => {
  it("composes both scheduling panels inside the shared shell and scopes activity", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/SchedulingManagement.vue"), "utf8");
    expect(source).toContain("SchedulerStrategyPanel");
    expect(source).toContain("SchedulerLogPanel");
    expect(source).toMatch(/#strategy=\"\{\s*active\s*\}\"/);
    expect(source).toMatch(/#logs=\"\{\s*active\s*\}\"/);
    expect(source).toContain(":active=\"active\"");
    expect(source).toContain("MediaWorkspaceShell");
  });
});
