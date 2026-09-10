import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
    listWorkOrders: vi.fn(),
    getWorkOrder: vi.fn(),
    stopWorkOrder: vi.fn(),
    deleteWorkOrder: vi.fn(),
    batchDeleteWorkOrders: vi.fn(),
    workOrderDownloadUrl: vi.fn((id: string) => `/api/gb28181/work-orders/${id}/download?token=t`),
    workOrderFileUrl: vi.fn((id: string, fileId: number) => `/api/gb28181/work-orders/${id}/files/${fileId}?token=t`)
}));
const message = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));
const account = vi.hoisted(() => ({ permissions: ["*:*:*"] as string[] }));

vi.mock("@/api/gb28181-work-recording", () => api);
vi.mock("@arco-design/web-vue", () => ({ Message: message, Modal: { confirm: modal.confirm } }));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));

import WorkOrders from "./index.vue";
import WorkOrderChannelsDialog from "./WorkOrderChannelsDialog.vue";

/** 表格只用来暴露行数；单元格交互由详情弹窗与源码契约覆盖。 */
const tableStub = defineComponent({
    name: "ATable",
    props: { data: { type: Array, default: () => [] } },
    setup(props) {
        return () =>
            h(
                "div",
                { "data-testid": "rows" },
                (props.data as Array<{ id: string }>).map(record => h("span", { class: "row", "data-id": record.id }, record.id))
            );
    }
});

const order = (overrides: Record<string, unknown> = {}) => ({
    id: "9b057ecd-215f-4caf-af40-6ca570ff1f62",
    requestId: "request-1",
    state: "stopped",
    formState: "submitted",
    formVersion: 1,
    projectName: "沪宁线放线作业",
    cameras: [
        {
            channelId: 12,
            channelName: "K12+300 左线",
            jobId: "job-1",
            state: "stopped",
            fileState: "ready",
            startedAt: "2026-09-10T08:12:05+08:00",
            stoppedAt: "2026-09-10T08:42:05+08:00",
            files: [
                { id: 88, channelId: 12, fileName: "12_20260910081200.mp4", startTime: "2026-09-10T08:12:00+08:00", timeLen: 300.5, fileSize: 52428800, state: "ready" }
            ]
        }
    ],
    ...overrides
});

/** 行内操作走全站统一的 `a-link`，与告警/云录像等页面的测试桩保持一致。 */
const linkStub = {
    emits: ["click"],
    template: "<a class='a-link' @click=\"$emit('click')\"><slot name='icon' /><slot /></a>"
};

function mountPage() {
    return mount(WorkOrders, { global: { stubs: { "a-table": tableStub, "a-table-column": { template: "<div />" }, "a-link": linkStub } } });
}

/**
 * `<script setup>` 暴露的状态在类型上不可见，测试通过这个视图读取，
 * 避免 `wrapper.vm.xxx` 触发 TS2339（build:prod 会跑 vue-tsc）。
 */
interface WorkOrdersPage {
    openDetail: (order: unknown) => Promise<void>;
    stop: (order: unknown) => Promise<void>;
    removeOne: (order: unknown) => Promise<void>;
    removeSelected: () => Promise<void>;
    openChannels: (order: unknown) => void;
    selectedIds: string[];
    channelsVisible: boolean;
}

function page(wrapper: { vm: unknown }): WorkOrdersPage {
    return wrapper.vm as unknown as WorkOrdersPage;
}

describe("work orders page", () => {
    beforeEach(() => {
        account.permissions = ["*:*:*"];
        api.listWorkOrders.mockResolvedValue({ code: 0, data: { items: [order()], total: 1, page: 1, pageSize: 10 }, message: "" });
        api.getWorkOrder.mockResolvedValue({
            code: 0,
            data: {
                snapshot: order(),
                form: { batchId: "9b057ecd-215f-4caf-af40-6ca570ff1f62", formVersion: 1, formState: "submitted", schemaVersion: 1, deviceId: "", editable: false, form: { projectName: "沪宁线放线作业", workLeader: "张伟", stationArea: "南京南—江宁", workPersonnel: ["张伟", "李强"] } }
            },
            message: ""
        });
        api.stopWorkOrder.mockResolvedValue({ code: 0, data: order({ state: "stopped" }), message: "" });
        api.deleteWorkOrder.mockResolvedValue({ code: 0, data: { deleted: 1 }, message: "" });
        api.batchDeleteWorkOrders.mockResolvedValue({ code: 0, data: { deleted: 1 }, message: "" });
        // 确认弹窗在单测里直接点「确定」，逐条弹窗的时机由 Modal.confirm 的调用断言覆盖。
        modal.confirm.mockImplementation((options: { onOk?: () => void }) => {
            options.onOk?.();
        });
    });

    it("loads the first page on mount", async () => {
        const wrapper = mountPage();
        await flushPromises();

        expect(api.listWorkOrders).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 10 }));
        // 页面自带的 data-testid 会覆盖桩的，所以按页面的标识选择。
        expect(wrapper.get("[data-testid=work-order-table]").findAll(".row")).toHaveLength(1);
    });

    it("refuses to call the API at all without the view permission", async () => {
        account.permissions = [];
        const wrapper = mountPage();
        await flushPromises();

        expect(api.listWorkOrders).not.toHaveBeenCalled();
        expect(wrapper.text()).toContain("无权查看作业单");
    });

    // 详情是把作业单与关联录像连起来看的唯一入口。
    it("shows the form fields and the archived slices with token-carrying links", async () => {
        const wrapper = mountPage();
        await flushPromises();
        await page(wrapper).openDetail(order());
        await flushPromises();

        expect(api.getWorkOrder).toHaveBeenCalledWith("9b057ecd-215f-4caf-af40-6ca570ff1f62");
        const text = wrapper.text();
        expect(text).toContain("沪宁线放线作业");
        expect(text).toContain("张伟、李强");
        expect(text).toContain("K12+300 左线");
        expect(text).toContain("12_20260910081200.mp4");

        // 浏览器直达的播放链接必须自带令牌
        const link = wrapper.get("a[href*='/files/88']");
        expect(link.attributes("href")).toContain("token=t");
        expect(api.workOrderFileUrl).toHaveBeenCalledWith("9b057ecd-215f-4caf-af40-6ca570ff1f62", 88);
    });

    it("stops a running order and refreshes the list", async () => {
        const running = order({ state: "recording" });
        api.listWorkOrders.mockResolvedValue({ code: 0, data: { items: [running], total: 1, page: 1, pageSize: 10 }, message: "" });
        const wrapper = mountPage();
        await flushPromises();

        await page(wrapper).stop(running);
        await flushPromises();

        expect(api.stopWorkOrder).toHaveBeenCalledWith("9b057ecd-215f-4caf-af40-6ca570ff1f62");
        expect(message.success).toHaveBeenCalled();
        expect(api.listWorkOrders.mock.calls.length).toBeGreaterThan(1);
    });

    it("surfaces a stop failure and still reloads from the server", async () => {
        const running = order({ state: "recording" });
        api.stopWorkOrder.mockRejectedValue(new Error("网络超时"));
        const wrapper = mountPage();
        await flushPromises();

        await page(wrapper).stop(running);
        await flushPromises();

        expect(message.error).toHaveBeenCalled();
        expect(api.listWorkOrders.mock.calls.length).toBeGreaterThan(1);
    });

    it("reports a load failure instead of showing a stale table", async () => {
        api.listWorkOrders.mockRejectedValue(new Error("作业单查询失败"));
        const wrapper = mountPage();
        await flushPromises();

        expect(wrapper.text()).toContain("作业单查询失败");
    });

    it("asks for confirmation before deleting one order", async () => {
        const wrapper = mountPage();
        await flushPromises();

        await page(wrapper).removeOne(order());
        await flushPromises();

        expect(modal.confirm).toHaveBeenCalled();
        expect(api.deleteWorkOrder).toHaveBeenCalledWith("9b057ecd-215f-4caf-af40-6ca570ff1f62");
        expect(message.success).toHaveBeenCalled();
        expect(api.listWorkOrders.mock.calls.length).toBeGreaterThan(1);
    });

    it("refuses to delete without the delete permission", async () => {
        account.permissions = ["gb28181:work-order:view"];
        const wrapper = mountPage();
        await flushPromises();

        await page(wrapper).removeOne(order());
        await flushPromises();

        expect(modal.confirm).not.toHaveBeenCalled();
        expect(api.deleteWorkOrder).not.toHaveBeenCalled();
    });

    it("forwards every ticked order to the batch delete endpoint", async () => {
        const first = order();
        const second = order({ id: "26483e4e-e108-4d2c-b4c3-312ff97e5e13" });
        api.listWorkOrders.mockResolvedValue({ code: 0, data: { items: [first, second], total: 2, page: 1, pageSize: 10 }, message: "" });
        const wrapper = mountPage();
        await flushPromises();

        page(wrapper).selectedIds = [first.id, second.id];
        await wrapper.vm.$nextTick();
        await page(wrapper).removeSelected();
        await flushPromises();

        expect(api.batchDeleteWorkOrders).toHaveBeenCalledWith([first.id, second.id]);
        expect(page(wrapper).selectedIds).toEqual([]);
    });

    // 进行中的作业单删不掉，提示必须说清「被跳过了多少张」而不是笼统地报成功。
    it("explains which orders the server skipped", async () => {
        const running = order({ id: "26483e4e-e108-4d2c-b4c3-312ff97e5e13", state: "recording" });
        api.listWorkOrders.mockResolvedValue({ code: 0, data: { items: [order(), running], total: 2, page: 1, pageSize: 10 }, message: "" });
        api.batchDeleteWorkOrders.mockResolvedValue({ code: 0, data: { deleted: 1, skipped: [running.id] }, message: "" });
        const wrapper = mountPage();
        await flushPromises();

        page(wrapper).selectedIds = [order().id, running.id];
        await wrapper.vm.$nextTick();
        await page(wrapper).removeSelected();
        await flushPromises();

        expect(message.success).toHaveBeenCalledWith(expect.stringContaining("跳过 1 张"));
    });

    it("warns in the confirmation when a ticked order is still recording", async () => {
        const running = order({ id: "26483e4e-e108-4d2c-b4c3-312ff97e5e13", state: "recording" });
        api.listWorkOrders.mockResolvedValue({ code: 0, data: { items: [order(), running], total: 2, page: 1, pageSize: 10 }, message: "" });
        const wrapper = mountPage();
        await flushPromises();

        page(wrapper).selectedIds = [running.id];
        await wrapper.vm.$nextTick();
        await page(wrapper).removeSelected();
        await flushPromises();

        expect(modal.confirm).toHaveBeenCalledWith(expect.objectContaining({ content: expect.stringContaining("正在录制") }));
    });

    // 通道数可点：弹窗要能说清录的是哪台设备的哪个通道。
    it("opens the channel detail dialog with the recorded cameras", async () => {
        const wrapper = mountPage();
        await flushPromises();

        const camera = {
            channelId: 12,
            channelCode: "34020000001320000010",
            channelName: "K12+300 左线",
            deviceId: "37010301021180000007",
            deviceName: "前置摄像头",
            jobId: "job-1",
            state: "stopped",
            fileState: "ready",
            startedAt: null,
            stoppedAt: null,
            files: []
        };
        page(wrapper).openChannels(order({ cameras: [camera] }));
        await flushPromises();

        expect(page(wrapper).channelsVisible).toBe(true);
        const dialog = wrapper.findComponent(WorkOrderChannelsDialog);
        expect(dialog.props("cameras")).toEqual([camera]);
        expect(dialog.props("orderId")).toBe("9b057ecd-215f-4caf-af40-6ca570ff1f62");
    });
});

// 行内操作与权限门禁通过源码契约锁定：表格单元格在单测环境里不可渲染。
describe("work orders page contract", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/work-orders/index.vue"), "utf8");

    it("routes every row action through the work-order API", () => {
        expect(source).toMatch(/@click="openDetail\(record\)"/);
        expect(source).toMatch(/function download\(order: WorkOrderSnapshot\)[\s\S]*?workOrderDownloadUrl\(order\.id\)/);
        expect(source).toMatch(/@click="stop\(record\)"/);
    });

    it("gates stopping behind the stop permission", () => {
        expect(source).toMatch(/const canStop = computed\(\(\) => hasPermission\("gb28181:work-order:stop"\)\)/);
        expect(source).toMatch(/v-if="canStop && isWorkOrderActive\(record\.state\)"/);
    });

    it("gates deletion behind the delete permission and never offers it for a running order", () => {
        expect(source).toMatch(/const canDelete = computed\(\(\) => hasPermission\("gb28181:work-order:delete"\)\)/);
        expect(source).toMatch(/v-model:selected-keys="selectedIds"/);
        expect(source).toMatch(/:row-selection="canDelete \? \{ type: 'checkbox', showCheckedAll: true \} : undefined"/);
        expect(source).toMatch(/data-testid="batch-delete"/);
        expect(source).toMatch(/@click="removeOne\(record\)"/);
        // 正在录制的行只出现「结束录像」，不会同时给出一个点了必然失败的删除按钮。
        expect(source).toContain('v-else-if="canDelete"');
        expect(source).toContain(':disabled="isWorkOrderActive(record.state) || deleting"');
    });

    it("opens the channel dialog from the channel count column", () => {
        expect(source).toMatch(/data-testid="channel-count"[\s\S]{0,400}@click="openChannels\(record\)"/);
        expect(source).toContain("<WorkOrderChannelsDialog");
    });

    /**
     * 操作列必须与其它列表页用同一套全局类（`uvp-table-actions` +
     * `uvp-table-action--*`，色值定义在 `style/model/uvp-ui-language.scss`），
     * 否则又会出现"只有这个页面长得不一样"。
     */
    it("uses the shared table action convention instead of bespoke buttons", () => {
        const start = source.indexOf('title="操作"');
        expect(start).toBeGreaterThan(-1);
        const column = source.slice(start, source.indexOf("</a-table-column>", start));
        expect(column).toContain('class="uvp-table-actions"');
        for (const modifier of ["detail", "download", "stop", "delete"]) {
            expect(column).toContain(`uvp-table-action uvp-table-action--${modifier}`);
        }
        expect(column).not.toContain("<a-button");
        // 右固定且居中，与告警/级联/录像计划等页面一致；窄屏取消固定。
        expect(source).toMatch(/title="操作" :width="\d+" align="center" :fixed="isMobile \? '' : 'right'"/);
    });

    // 「详情」是作业单完整信息（表单字段 + 通道 + 录像分片）的唯一入口，
    // 曾经叫「查看录像」但打开的其实是详情弹窗，名字与功能不匹配，已正名。
    it("names the first action as the detail entry and opens the detail modal", () => {
        const start = source.indexOf('title="操作"');
        const column = source.slice(start, source.indexOf("</a-table-column>", start));
        expect(column).toContain('data-testid="detail"');
        expect(column).toContain("<span>详情</span>");
        expect(column).not.toContain("查看录像");
        expect(source).toContain('title="作业单详情"');
    });
    // 「录入后寄存，下拉选用」：4 个字段都支持下拉选用。
    // 3 个单值字段用 a-auto-complete（输入+下拉）；作业人员用 a-select multiple（多选下拉，
    // Arco 自带 tag 显示与删除，下拉选项自动排除已选）。
    it("supports the four history-remembered fields via dropdowns", () => {
        const dialog = readFileSync(resolve(process.cwd(), "src/views/gb28181/work-orders/WorkOrderFormDialog.vue"), "utf8");
        expect(dialog).toContain("workPersonnel");
        expect(dialog).toContain("personnelList");
        expect(dialog).toContain("personnelOptions");
        expect(dialog).toContain("listWorkOrderFormHistory");
        // 3 个单值字段必须走 historySingleFields 分支
        expect(dialog).toContain("historySingleFields");
        // 4 字段数据来源统一：historyByField
        expect(dialog).toContain("historyByField");
        // 组件统计：3 个单值字段共用 1 个 v-else-if 里的 a-auto-complete 标签；
        // 作业人员用 1 个 a-select multiple；a-textarea 仅剩 2 个 longFields（wireLayingProcess/remark）。
        const aInputCount = (dialog.match(/<a-input\b/g) || []).length;
        const aTextAreaCount = (dialog.match(/<a-textarea\b/g) || []).length;
        const aAutoCompleteCount = (dialog.match(/<a-auto-complete\b/g) || []).length;
        const aSelectMultiple = /<a-select\b[\s\S]*?multiple[\s\S]*?\/>/.test(dialog);
        expect(aAutoCompleteCount).toBe(1);
        expect(aTextAreaCount).toBe(1);  // 1 个 v-else-if 分支（runtime 二选一实例化 1 次）
        expect(aInputCount).toBeGreaterThanOrEqual(1);
        expect(aSelectMultiple).toBe(true);
        // 作业人员 v-model 直接绑 personnelList（数组）—— 不再有手动追加逻辑
        expect(dialog).toContain('v-model="personnelList"');
    });


    // 弹窗的四列是需求原样：设备名称 / 设备ID / 通道名称 / 通道ID。
    it("labels the channel dialog columns exactly as required", () => {
        const dialog = readFileSync(resolve(process.cwd(), "src/views/gb28181/work-orders/WorkOrderChannelsDialog.vue"), "utf8");
        for (const title of ["设备名称", "设备ID", "通道名称", "通道ID"]) {
            expect(dialog).toContain(`title="${title}"`);
        }
        expect(dialog).toContain('modal-class="uvp-system-dialog"');
        expect(dialog).toContain('class="uvp-data-table"');
    });

    it("uses the shared system dialog and table classes for UI consistency", () => {
        expect(source).toContain('modal-class="uvp-system-dialog"');
        expect(source).toContain('class="uvp-data-table"');
        expect(source).toContain("<s-layout-search>");
    });
});

/**
 * label 对齐：dialog 给所有 a-form-item 强制同一 label-col 宽度，
 * 让 "专业"(2字) 与 "恒张力放线车型号"(8字) 同一行的两列、跨所有行的 input
 * 左边缘都对齐到同一条垂直线。否则 Arco 按 label 文本自适应，会逐行漂移。
 */
describe("work order form dialog", () => {
    const dialogSource = readFileSync(resolve(__dirname, "WorkOrderFormDialog.vue"), "utf8");

    it("fixes the horizontal label-col width so all inputs share a left edge", () => {
        expect(dialogSource).toMatch(/LABEL_COL_WIDTH\s*=\s*\d+/);
        // form-item must apply the fixed label-col-style; otherwise Arco auto-sizes per label.
        expect(dialogSource).toMatch(/:label-col-style="labelColStyle"/);
        expect(dialogSource).toMatch(/:wrapper-col-style="wrapperColStyle"/);
        // Width must accommodate the longest Chinese label (8 chars).
        const widthMatch = dialogSource.match(/LABEL_COL_WIDTH\s*=\s*(\d+)/);
        expect(widthMatch).not.toBeNull();
        expect(Number(widthMatch![1])).toBeGreaterThanOrEqual(100);
    });

    it("keeps the record-scope banner content flush with form field labels", () => {
        // The banner's body uses an inline margin-left derived from LABEL_COL_WIDTH
        // so the title and chips line up with the input left edge of the first form row.
        expect(dialogSource).toMatch(/marginLeft.*LABEL_COL_WIDTH/);
    });
});
