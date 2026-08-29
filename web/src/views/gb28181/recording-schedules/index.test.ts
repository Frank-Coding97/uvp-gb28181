import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/index.vue"), "utf8");
const drawerSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/RecordingScheduleDrawer.vue"), "utf8");

describe("recording schedule prototype", () => {
  it("uses the system list-page shell while preserving the segmented view switch", () => {
    expect(source).toContain("录像计划");
    expect(source).toContain("计划管理");
    expect(source).toContain("通道执行状态");
    expect(source).toContain('class="segmented schedule-view-switch"');
    expect(source).toContain("<s-layout-search>");
    expect(source).toContain('class="uvp-data-table schedule-data-table"');
    expect(source).not.toContain("prototype-alert");
    expect(source).not.toContain("prototype-table");
  });

  it("shows the agreed management fields and operation entry points", () => {
	expect(source).toContain("listRecordingPlans");
	expect(source).not.toContain('id: "workday"');
    expect(source).toContain("执行周期");
    expect(source).toContain("录像时段");
    expect(source).toContain("已应用通道");
    expect(source).toContain("新建计划");
    expect(source).toContain("分配通道");
    expect(source).toContain("查看");
    expect(source).toContain("编辑");
  });

  it("inherits the standard control heights instead of overriding them to 44px", () => {
    const searchControlRule = source.match(/\.recording-schedules-page :deep\(\.uvp-search-panel \.arco-select-view\)\s*\{([^}]*)\}/s);
    const toolbarButtonRule = source.match(/\.recording-schedules-page :deep\(\.toolbar-actions \.arco-btn\)\s*\{([^}]*)\}/s);
    expect(searchControlRule).not.toBeNull();
    expect(toolbarButtonRule).not.toBeNull();
    expect(searchControlRule?.[1]).not.toMatch(/\b(?:min-)?height\s*:/);
    expect(toolbarButtonRule?.[1]).not.toMatch(/\b(?:min-)?height\s*:/);
  });

  it("removes plan copying from the ledger and its implementation", () => {
    expect(source).not.toContain("uvp-table-action--copy");
    expect(source).not.toContain("@click=\"copyPlan(record)\"");
    expect(source).not.toContain("function copyPlan");
    expect(source).not.toMatch(/\bCopy\b/);
    expect(source).not.toContain("计划已复制");
  });

  it("places plan status immediately after the applied-channel column", () => {
    const appliedChannelColumn = source.indexOf('<a-table-column title="已应用通道"');
    const statusColumn = source.indexOf('<a-table-column title="状态"');
    const updatedAtColumn = source.indexOf('<a-table-column title="最近更新"');
    expect(appliedChannelColumn).toBeGreaterThan(-1);
    expect(statusColumn).toBeGreaterThan(appliedChannelColumn);
    expect(statusColumn).toBeLessThan(updatedAtColumn);
  });

  it("controls plan enablement directly from the status-column switch", () => {
    const statusColumn = source.slice(
      source.indexOf('<a-table-column title="状态"'),
      source.indexOf('<a-table-column title="最近更新"')
    );
    expect(statusColumn).toContain('<a-switch v-model="record.enabled"');
    expect(statusColumn).toContain('@change="notifyPlanStatusChange(record)"');
    expect(statusColumn).not.toContain("<a-tag");
    expect(source).toContain("function notifyPlanStatusChange");
  });

  it("opens the channel assignment dialog from the applied-channel count", () => {
    expect(source).toContain('class="channel-count-link"');
    expect(source).toContain('@click="openAssign(record)"');
    expect(source).toContain('查看并管理 ${record.channelCount} 个已应用通道');
    expect(source).toContain('v-model:visible="assignmentVisible"');
    expect(source).toContain(':plan-id="activePlan?.id"');
    expect(source).toContain("activePlan.value = plan");
    expect(source).toContain("assignmentVisible.value = true");
  });

  it("makes the applied-channel count visibly identifiable as a link", () => {
    expect(source).toMatch(/\.channel-count-link\s*{[^}]*color:\s*var\(--uvp-brand-strong\)\s*!important;[^}]*cursor:\s*pointer;[^}]*text-decoration:\s*underline;/s);
    expect(source).toContain(".channel-count-link:hover");
    expect(source).toContain("background: var(--uvp-brand-soft)");
  });

  it("keeps channel assignment on plan rows and hides the fixed China timezone", () => {
    expect(source).toContain('uvp-table-action--assign');
    expect(source).not.toContain('@click="openAssign()"');
    expect(source).not.toContain("timezone-hint");
    expect(source).not.toContain('title="时区"');
    expect(source).not.toContain("Asia/Shanghai");
  });

  it("keeps plan expectation and actual recording state visually distinct", () => {
    expect(drawerSource).toContain("当前时段");
    expect(source).toContain("实际状态");
    expect(source).toContain("录像中");
    expect(source).toContain("等待设备上线");
    expect(source).toContain("时段外");
    expect(source).toContain("关闭");
    expect(source).toContain("持续录像");
    expect(source).toContain("按计划");
  });

  it("uses system dialogs for detail, create, edit, and channel assignment", () => {
    expect(source).toContain("RecordingScheduleDrawer");
    expect(source).toContain("RecordingScheduleEditorDialog");
    expect(source).toContain("ChannelAssignmentDialog");
    expect(source).not.toContain("ChannelAssignmentDrawer");
    expect(drawerSource).toContain("<a-modal");
    expect(drawerSource).not.toContain("<a-drawer");
    expect(source).toContain('mode="detail"');
    expect(source).toContain(':mode="editorMode"');
    expect(source).not.toContain('@edit="openEdit" @assign="openAssignFromDrawer"');
    expect(source).not.toContain("function openAssignFromDrawer");
  });

  it("uses the standard full-height page shell without leaking page scroll", () => {
    expect(source).toContain('class="snow-fill recording-schedules-page"');
    expect(source).toContain('class="snow-fill-inner uvp-page-shell-flat recording-schedules-shell"');
    expect(source).toMatch(/\.recording-schedules-page\s*{[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.recording-schedules-shell\s*{[^}]*display:\s*flex;[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*flex-direction:\s*column;[^}]*overflow:\s*hidden;/s);
  });

  it("uses a plan-first split workspace for channel execution status", () => {
    expect(source).toContain('class="execution-workspace"');
    expect(source).toContain('class="execution-plan-panel"');
    expect(source).toContain('class="execution-plan-list"');
    expect(source).toContain('class="execution-channel-panel"');
    expect(source).toContain('@click="selectExecutionPlan(plan.id)"');
    expect(source).toContain("selectedExecutionPlanId");
    expect(source).toContain("selectedExecutionPlan");
	expect(source).toContain("loadExecutionChannels");
	expect(source).toContain("listRecordingPlanExecutionChannels");
    expect(source).toContain("当前计划关联通道");
    expect(source).not.toContain('placeholder="关联计划"');
  });

  it("loads plans incrementally and uses the system search-panel input treatment", () => {
    expect(source).toContain('<s-layout-search class="execution-plan-search">');
    expect(source).toContain('@scroll.passive="handleExecutionPlanScroll"');
    expect(source).toContain("const executionPlanPageSize = 30");
	expect(source).toContain("executionPlanPage");
    expect(source).toContain("hasMoreExecutionPlans");
    expect(source).toContain("loadMoreExecutionPlans");
	expect(source).toContain("已加载 {{ visibleExecutionPlans.length }} / 共 {{ executionPlanTotal }} 条");
    expect(source).toContain(".execution-plan-search :deep(.uvp-search-panel__surface)");
  });

  it("paginates the recording plan ledger for server-side data", () => {
    expect(source).toContain(':pagination="planPagination"');
    expect(source).toContain('@page-change="handlePlanPageChange"');
    expect(source).toContain('@page-size-change="handlePlanPageSizeChange"');
    expect(source).toContain("showPageSize: true");
    expect(source).toContain("showJumper: true");
  });

  it("accepts recording stream context only as an existing permission-scoped filter", () => {
    expect(source).toContain("useRoute");
    expect(source).toContain("recordingContextKeyword");
    expect(source).toContain("route.query.stream");
    expect(source).toContain('activeView = ref<"plans" | "status">(recordingContextKeyword ? "status" : "plans")');
    expect(source).toContain("数据仍由录像计划接口按原权限返回");
    expect(source).toContain("listRecordingPlanExecutionChannels");
  });
});
