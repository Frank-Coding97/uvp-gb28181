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

  it("moves node selection into a lightweight runtime context toolbar", () => {
    const page = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaOverview.vue"), "utf8");
    const panel = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");

    expect(page).toContain('import MediaScopeBar from "./components/MediaScopeBar.vue"');
    expect(page).toContain(':show-scope="false"');
    expect(page).toContain("<template #scope>");
    expect(page).toContain("<MediaScopeBar");
    expect(page).toContain("compact");
    expect(panel).toContain('class="runtime-context-toolbar"');
    expect(panel).toContain('<slot name="scope" />');
    expect(panel).toContain("数据更新于");
    expect(panel).toContain('aria-label="刷新节点运行态"');
    expect(panel).toMatch(/\.runtime-context-toolbar\s*\{[^}]*border-bottom:\s*1px solid var\(--zlm-border\)/);
    expect(panel).toMatch(/\.runtime-context-toolbar\s*\{[^}]*background:\s*transparent/);
    expect(panel).toContain("@keyframes runtime-context-spin");
  });

  it("does not add a redundant top margin inside the scrollable runtime panel", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/monitoring/RuntimeSummaryPanel.vue"), "utf8");
    expect(source).toMatch(/\.runtime-summary-kpis\s*\{[^}]*margin-top:\s*0/);
    expect(source).toMatch(/\.runtime-summary-grid\s*\{[^}]*margin-top:\s*8px/);
    expect(source).toMatch(/\.runtime-thread-panel--full\s*\{[^}]*margin-top:\s*8px[^}]*padding-bottom:\s*10px/);
  });
});
