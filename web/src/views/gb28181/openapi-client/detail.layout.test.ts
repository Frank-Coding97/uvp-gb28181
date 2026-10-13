import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const detailSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/openapi-client/detail.vue"), "utf8");
const listSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/openapi-client/index.vue"), "utf8");
const workbenchSource = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/openapi-client/OpenAPICapabilityWorkbench.vue"),
  "utf8"
);

describe("OpenAPI client detail information architecture", () => {
  it("keeps capability authorization as the only detail tab", () => {
    expect(detailSource).not.toContain('name: "overview"');
    expect(detailSource).toContain('name: "capabilities"');
    expect(detailSource).not.toContain('name: "logs"');
    expect(detailSource).not.toContain('key="overview"');
    expect(listSource).toContain("调用记录");
    expect(listSource).toContain("openOverview(record)");
    expect(listSource).toContain("OpenAPIClientOverviewDialog");
    expect(listSource).toContain("openWorkspace(record, 'logs')");
  });

  it("keeps credential and lifecycle actions on the client list", () => {
    expect(detailSource).not.toContain('key="security"');
    expect(detailSource).not.toContain("凭证与安全");
    expect(detailSource).not.toContain("轮换 SK");
    expect(detailSource).not.toContain("观看连接清退");
    expect(listSource).toContain("requestRotate(record)");
    expect(listSource).toContain("requestStatus('disable', record)");
    expect(listSource).toContain("requestStatus('enable', record)");
    expect(listSource).toContain("requestStatus('revoke', record)");
  });

  it("uses the shared system UI components instead of a custom page language", () => {
    expect(detailSource).toContain('class="uvp-system-panel openapi-detail__summary"');
    expect(detailSource).not.toContain('class="uvp-system-tabs openapi-detail__tabs"');
    expect(detailSource).not.toContain("<a-tabs");

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

  it("keeps overview out of the detail workspace", () => {
    expect(detailSource).not.toContain("查看能力授权");
    expect(detailSource).not.toContain("查看调用记录");
    expect(detailSource).not.toContain("openapi-detail__overview-actions");
    expect(detailSource).not.toContain("openapi-detail__overview");
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
    expect(detailSource).toContain('<time :datetime="record.createdAt">{{ formatTime(record.createdAt) }}</time>');
    expect(detailSource).not.toContain("<h2>调用记录</h2>");
    expect(detailSource).not.toContain('class="openapi-detail__logs-toolbar"');
    expect(detailSource).not.toContain('class="uvp-system-panel openapi-detail__panel" :bordered="false" title="调用记录"');
  });

  it("keeps outer capability panels stable and delegates vertical scrolling to the capability table", () => {
    expect(workbenchSource).toMatch(/\.capability-workbench__body\s*{[^}]*height: clamp\(480px/s);
    expect(workbenchSource).toMatch(/\.capability-workbench__list\s*{[^}]*overflow: hidden/s);
    expect(workbenchSource).toContain(':scroll="{ x: 900, y: 440 }"');
    expect(workbenchSource).toMatch(
      /@media \(width <= 1180px\)[\s\S]*\.capability-workbench__body\s*{[^}]*height: clamp\(480px/s
    );
    expect(workbenchSource).toContain("grid-template-rows: minmax(0, 1fr) 180px");
    expect(workbenchSource).toContain(".capability-workbench__change-scroll");
    expect(workbenchSource).toMatch(/\.capability-workbench__changes :deep\(\.arco-card-body\)\s*{[^}]*box-sizing: border-box/s);
    expect(workbenchSource).toMatch(/\.capability-workbench__actions\s*{[^}]*flex: 0 0 auto/s);
    expect(workbenchSource).toMatch(/@media \(width <= 760px\)[\s\S]*\.capability-workbench__body\s*{[^}]*height: auto/s);
  });
});
