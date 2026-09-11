import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/RecordingScheduleEditorDialog.vue"), "utf8");
const gridSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/WeeklyScheduleGrid.vue"), "utf8");

describe("RecordingScheduleEditorDialog half-hour editor", () => {
  it("uses a system dialog instead of a drawer", () => {
    expect(source).toContain("<a-modal");
    expect(source).toContain('modal-class="uvp-system-dialog schedule-editor-dialog"');
    expect(source).toContain("新建录像计划");
    expect(source).toContain("编辑录像计划");
    expect(source).not.toContain("<a-drawer");
  });

  it("edits a week with 48 half-hour slots and paint interactions", () => {
    expect(source).toContain('<WeeklyScheduleGrid :slots="weekSlots" editable');
    expect(gridSource).toContain("const SLOT_MINUTES = 30");
    expect(gridSource).toContain("Array.from({ length: 48 }");
    expect(gridSource).toContain("@mousedown");
    expect(gridSource).toContain("@mouseenter");
    expect(source).not.toContain(">复制</a-button>");
    expect(source).not.toContain(">粘贴</a-button>");
    expect(source).toContain("全天");
    expect(source).toContain("清空");
    expect(source).not.toContain('v-model="period.start"');
    expect(source).not.toContain('v-model="period.end"');
  });

  it("clears the whole week with one action and renders visible grid borders", () => {
    expect(source).toContain('@click="clearAllDays"');
    expect(source).toContain("一键清空");
    expect(source).toContain("function clearAllDays()");
    expect(source).toContain('class="clear-all-button" size="small" type="primary" status="danger"');
    expect(gridSource).toContain("border: 1px solid var(--color-border-3)");
    expect(gridSource).toContain("border-right: 1px solid var(--color-border-3)");
  });

  it("removes the dependent copy and paste interaction", () => {
    expect(source).not.toContain("copiedDayIndex");
    expect(source).not.toContain("function copyDay");
    expect(source).not.toContain("function pasteDay");
  });

  it("shows the exact selected intervals grouped by weekday", () => {
    expect(source).toContain("已选时间区间");
    expect(source).toContain("selectedPeriodGroups");
    expect(source).toContain("selected-period-summary");
    expect(source).toContain("selected-period-chip");
    expect(source).toContain("暂未选择录像时段");
  });

  it("caps dialog and interval-summary height so dense selections cannot grow the modal", () => {
    expect(source).toContain(":modal-style");
    expect(source).toContain(":body-style");
    expect(source).toContain("calc(100vh - 48px)");
    expect(source).toContain("calc(100vh - 176px)");
    expect(source).toContain("height: 148px");
    expect(source).toContain("overflow-y: auto");
  });

  it("fits the full grid on common desktops and keeps action buttons visible when scrolling", () => {
    expect(source).toContain("min(1360px, 98vw)");
    expect(gridSource).toContain("min-width: 1026px");
    expect(gridSource).toContain("repeat(24, minmax(36px, 1fr))");
    expect(gridSource).toContain("position: sticky");
    expect(gridSource).toContain("right: 0");
  });

  it("renders an overnight legacy period across the current and following weekday", () => {
    expect(source).toContain("(dayIndex + 1) % dayNames.length");
    expect(source).not.toContain("runs.shift()");
    expect(source).not.toContain("runs.pop()");
  });
});
