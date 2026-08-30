import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("MediaOverview canonical workbench", () => {
  it("renders the shared shell and the single canonical overview panel", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaOverview.vue"), "utf8");
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain("MediaOverviewPanel");
    expect(source).toContain(":auto-refresh");
    expect(source).not.toContain("getZLMOverview");
  });
});
