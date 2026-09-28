import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const detailSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/openapi-client/detail.vue"), "utf8");
const workbenchSource = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/openapi-client/OpenAPICapabilityWorkbench.vue"),
  "utf8"
);

describe("OpenAPI client detail information architecture", () => {
  it("separates overview, capability, credential security and call records", () => {
    expect(detailSource).toContain('name: "overview"');
    expect(detailSource).toContain('name: "capabilities"');
    expect(detailSource).toContain('name: "security"');
    expect(detailSource).toContain('name: "logs"');
    expect(detailSource).toContain("调用记录");
  });

  it("uses the shared system UI components instead of a custom page language", () => {
    expect(detailSource).toContain('class="uvp-system-panel openapi-detail__summary"');
    expect(detailSource).toContain('class="uvp-system-tabs openapi-detail__tabs"');
    expect(detailSource).toContain('class="uvp-system-description"');
    expect(detailSource).not.toContain('<nav class="openapi-detail__tabs"');

    expect(workbenchSource).toContain("<s-layout-search");
    expect(workbenchSource).toContain("<a-menu");
    expect(workbenchSource).toContain('class="uvp-data-table capability-workbench__table"');
    expect(workbenchSource).toContain('class="uvp-system-panel capability-workbench__changes"');
    expect(workbenchSource).not.toContain("<article");
  });

  it("keeps the back action and client summary in one compact header row", () => {
    expect(detailSource).toContain('class="openapi-detail__summary-back"');
    expect(detailSource).toContain('class="openapi-detail__summary-content"');
    expect(detailSource).not.toContain('<div class="openapi-detail__back">');
  });

  it("keeps audit and revocation concerns out of the capability workbench", () => {
    expect(workbenchSource).not.toContain("调用审计");
    expect(workbenchSource).not.toContain("清退状态");
    expect(workbenchSource).toContain("本次变更");
    expect(workbenchSource).toContain("仅看已授权");
  });

  it("renders call records as a paged data table without an outer card", () => {
    expect(detailSource).toContain('<s-layout-search class="openapi-detail__logs-filter-bar">');
    expect(detailSource).toContain(':pagination="auditPagination"');
    expect(detailSource).toContain('@page-change="handleAuditPageChange"');
    expect(detailSource).toContain('@page-size-change="handleAuditPageSizeChange"');
    expect(detailSource).not.toContain("<h2>调用记录</h2>");
    expect(detailSource).not.toContain('class="openapi-detail__logs-toolbar"');
    expect(detailSource).not.toContain('class="uvp-system-panel openapi-detail__panel" :bordered="false" title="调用记录"');
  });

  it("bounds long desktop capability catalogs while keeping narrow layouts naturally scrollable", () => {
    expect(workbenchSource).toMatch(/\.capability-workbench__body\s*{[^}]*height: clamp\(/s);
    expect(workbenchSource).toMatch(/\.capability-workbench__list\s*{[^}]*overflow: auto/s);
    expect(workbenchSource).toMatch(/@media \(width <= 1180px\)[\s\S]*\.capability-workbench__body\s*{[^}]*height: auto/s);
  });
});
