import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/RecordingScheduleDrawer.vue"), "utf8");

describe("RecordingScheduleDrawer high fidelity workflow", () => {
  it("keeps the read-only plan detail and weekly timeline in a system dialog", () => {
    expect(source).toContain("<a-modal");
    expect(source).toContain('modal-class="uvp-system-dialog recording-schedule-detail-dialog"');
    expect(source).not.toContain("<a-drawer");
    expect(source).toContain("计划详情");
    expect(source).toContain("每周时段");
    expect(source).toContain("应用通道");
    expect(source).not.toContain("执行时区");
    expect(source).not.toContain("Asia/Shanghai");
    expect(source).not.toContain("保存计划");
  });

  it("keeps detail actions in the fixed dialog footer", () => {
    expect(source).toContain('<template #footer>');
    expect(source).toContain(">关闭</a-button>");
    expect(source).not.toContain(">分配通道</a-button>");
    expect(source).not.toContain(">编辑计划</a-button>");
    expect(source).not.toContain(">管理通道</a-link>");
    expect(source).toContain("flex: '1 1 auto'");
    expect(source).toContain("minHeight: 0");
  });

  it("suppresses the outer modal-wrapper scrollbar", () => {
    expect(source).toContain(".arco-modal-wrapper:has(> .recording-schedule-detail-dialog)");
    expect(source).toMatch(/\.arco-modal-wrapper:has\(> \.recording-schedule-detail-dialog\)\s*{[^}]*overflow:\s*hidden;/s);
  });

  it("separates recording configuration and assigned channels into tabs", () => {
    expect(source).toContain('<a-tabs v-model:active-key="activeDetailTab"');
    expect(source).toContain('<a-tab-pane key="schedule"');
    expect(source).toContain('<a-tab-pane key="channels"');
    expect(source).toContain("录像配置");
    expect(source).toContain("已分配通道");
    expect(source).toContain('activeDetailTab.value = "schedule"');

    const schedulePane = source.slice(source.indexOf('<a-tab-pane key="schedule"'), source.indexOf('<a-tab-pane key="channels"'));
    const channelsPane = source.slice(source.indexOf('<a-tab-pane key="channels"'), source.indexOf('</a-tabs>'));
    expect(schedulePane).toContain("每周时段");
    expect(schedulePane).toContain('<WeeklyScheduleGrid :slots="detailWeekSlots" :editable="false" />');
    expect(schedulePane).not.toContain("assigned-channel-table");
    expect(channelsPane).toContain("assigned-channel-table");
  });

  it("uses the same weekly grid component as the editor in read-only mode", () => {
    expect(source).toContain('import WeeklyScheduleGrid from "./WeeklyScheduleGrid.vue"');
    expect(source).toContain("const detailWeekSlots = computed");
    expect(source).not.toContain("schedule-track");
    expect(source).not.toContain("schedule-segment");
  });

	it("receives assigned channels from the parent instead of embedding demo rows", () => {
		expect(source).toContain("assignedChannels?: ScheduleChannel[]");
		expect(source).toContain(":loading=\"channelsLoading\"");
		expect(source).not.toContain('id: "c1"');
	});
});
