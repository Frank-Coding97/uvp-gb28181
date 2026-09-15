import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("MediaOverview canonical workbench", () => {
  it("renders the shared shell and the concrete-node runtime panel", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaOverview.vue"), "utf8");
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain("RuntimeSummaryPanel");
    expect(source).toContain(':requires-node="true"');
    expect(source).toContain(':allow-all="false"');
    expect(source).toContain(':show-toolbar-actions="false"');
    expect(source).not.toContain("getZLMOverview");
  });

  it("does not add a redundant top margin inside the scrollable runtime panel", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");
    expect(source).toMatch(/\.runtime-summary-kpis\s*\{[^}]*margin-top:\s*0/);
    expect(source).toMatch(/\.runtime-summary-grid\s*\{[^}]*margin-top:\s*8px/);
    expect(source).toMatch(/\.runtime-thread-panel--full\s*\{[^}]*margin-top:\s*8px[^}]*padding-bottom:\s*10px/);
  });
});
