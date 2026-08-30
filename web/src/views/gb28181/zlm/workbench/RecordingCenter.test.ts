import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/RecordingCenter.vue"), "utf8");
const files = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/recording/RecordingFilesPanel.vue"), "utf8");
const tasks = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/recording/RecordingTasksPanel.vue"), "utf8");
const summary = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/recording/RecordingSummaryStrip.vue"), "utf8");

describe("RecordingCenter", () => {
  it("maps files, tasks and plans to one active panel each", () => {
    expect(source).toContain("RecordingFilesPanel");
    expect(source).toContain("RecordingTasksPanel");
    expect(source).toContain("RecordingPlansPanel");
    expect(source).toContain("RecordingSummaryStrip");
    expect(source).toContain(":active=\"workspace.activeView.value === 'files'\"");
    expect(source).toContain(":active=\"workspace.activeView.value === 'tasks'\"");
    expect(source).toContain(":active=\"workspace.activeView.value === 'plans'\"");
  });

  it("reuses one route-independent cloud implementation", () => {
    expect(files).toContain('mode="files"');
    expect(tasks).toContain('mode="tasks"');
    expect(files).toContain("cloud-recordings/RecordingWorkspacePanel.vue");
    expect(tasks).toContain("cloud-recordings/RecordingWorkspacePanel.vue");
    expect(files).not.toContain("listRecordingFiles");
    expect(tasks).not.toContain("listActiveRecordings");
  });

  it("does not invent storage capacity or a global abnormal count", () => {
    expect(source).not.toMatch(/磁盘使用率|存储容量|globalAbnormal/i);
    expect(source).toContain("当前计划异常");
    expect(summary).toContain("未加载 / 无权限");
  });
});
