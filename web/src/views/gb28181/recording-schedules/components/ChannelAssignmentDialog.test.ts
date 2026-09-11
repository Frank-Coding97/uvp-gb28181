import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/ChannelAssignmentDialog.vue"), "utf8");

describe("ChannelAssignmentDialog high fidelity workflow", () => {
  it("uses a system dialog instead of a drawer", () => {
    expect(source).toContain("<a-modal");
    expect(source).toContain('modal-class="uvp-system-dialog channel-assignment-dialog"');
    expect(source).toContain("align-center");
    expect(source).toContain("height: 'min(760px, calc(100vh - 48px))'");
    expect(source).toContain("display: 'flex'");
    expect(source).toContain("flex: '1 1 auto'");
    expect(source).toContain("minHeight: 0");
    expect(source).not.toContain("<a-drawer");
  });

  it("locks the plan inherited from the ledger row and makes assignment impact explicit", () => {
    expect(source).toContain("分配录像计划");
    expect(source).toContain("当前录像计划");
    expect(source).toContain("selectedPlan.name");
    expect(source).not.toContain("选择录像计划");
    expect(source).not.toContain('v-model="selectedPlanId"');
    expect(source).toContain("选择后，通道录像模式将切换为“按计划”");
    expect(source).toContain("确认分配");
    expect(source).toContain("当前计划已停用，不能新增分配");
    expect(source).toContain("!selectedPlan?.enabled");
  });

  it("supports device and channel scopes without loading a device dropdown", () => {
    expect(source).toContain("按设备");
    expect(source).toContain("按通道");
    expect(source).toContain("selectionScope");
    expect(source).toContain("输入设备名称或国标编码");
    expect(source).toContain("输入通道名称、编码或所属设备");
    expect(source).not.toContain("deviceFilter");
    expect(source).not.toContain('placeholder="所属设备"');
  });

  it("places the online-state dropdown in the search toolbar", () => {
    const filters = source.slice(source.indexOf('<div class="assignment-filters">'), source.indexOf('<div class="selection-policy">'));
    expect(filters).toContain('<a-select v-model="onlineFilter"');
    expect(filters).toContain(':options="onlineFilterOptions"');
    expect(source).toContain("仅显示在线通道");
    expect(source).not.toContain('<a-checkbox v-model="onlineOnly">');
  });

  it("paginates the assignment ledger and explains page-scoped select-all", () => {
    expect(source).toContain(':pagination="assignmentPagination"');
    expect(source).toContain('@page-change="handlePageChange"');
    expect(source).toContain('@page-size-change="handlePageSizeChange"');
    expect(source).toContain("全选仅作用于当前页");
    expect(source).toContain("跨页保留");
  });

	it("loads assignment candidates and confirms selection through real APIs", () => {
		expect(source).toContain("listRecordingPlanDevices");
		expect(source).toContain("listRecordingPlanChannels");
		expect(source).toContain("assignRecordingPlan");
		expect(source).not.toContain("const devices:");
		expect(source).not.toContain("const channels:");
	});
});
