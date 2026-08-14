import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const alarmApi = vi.hoisted(() => ({ listAlarms: vi.fn(), deleteAlarm: vi.fn(), batchDeleteAlarms: vi.fn(), clearAllAlarms: vi.fn() }));
const deviceApi = vi.hoisted(() => ({ listDevices: vi.fn() }));
const account = vi.hoisted(() => ({ permissions: ["gb28181:alarm:view"] as string[] }));
const modal = vi.hoisted(() => ({ warning: vi.fn() }));
const messages = vi.hoisted(() => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn() }));

vi.mock("./api", async importOriginal => ({
  ...(await importOriginal<typeof import("./api")>()),
  ...alarmApi
}));
vi.mock("../device-mgmt/api", async importOriginal => ({
  ...(await importOriginal<typeof import("../device-mgmt/api")>()),
  ...deviceApi
}));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@arco-design/web-vue", async importOriginal => ({
  ...(await importOriginal<typeof import("@arco-design/web-vue")>()),
  Modal: modal
}));
vi.mock("@/hooks/useGlobalProperties", () => ({
  default: () => ({ $message: messages })
}));

import { getLucideIconComponent } from "@/utils/lucide-menu-icons";
import AlarmManagement from "./index.vue";

const listResult = (id = "9007199254740993") => ({
  code: 0,
  message: "ok",
  data: {
    list: [
      {
        id,
        receivedAt: "2026-08-04T17:49:11+08:00",
        alarmTime: "2026-08-04T17:48:00+08:00",
        device: { id: 12, code: "37010301021320000111", name: "NVR", alias: "一号机房" },
        channel: { id: 31, code: "37010301021320000112", name: "通道1", alias: "东门" },
        sourceCode: "37010301021320000112",
        priority: { value: 1, label: "一级警情" },
        method: { value: 5, label: "视频报警" },
        alarmType: { value: 2, label: "运动目标检测报警" },
        description: "检测到移动目标"
      }
    ],
    total: 41,
    page: 1,
    pageSize: 10
  }
});

const stubs = {
  "s-layout-search": { template: "<section><slot name='fields' /><slot name='actions' /><slot name='extra' /></section>" },
  "a-input": {
    props: ["modelValue", "placeholder"],
    emits: ["update:modelValue", "pressEnter"],
    template:
      "<input :data-testid='$attrs[`data-testid`]' :data-placeholder='placeholder' :value='modelValue' @input='$emit(`update:modelValue`, $event.target.value)' @keyup.enter='$emit(`pressEnter`)' />"
  },
  "a-select": {
    props: ["modelValue", "disabled", "placeholder"],
    emits: ["update:modelValue", "search"],
    template:
      "<select :data-testid='$attrs[`data-testid`]' :data-placeholder='placeholder' :value='modelValue' :disabled='disabled' @change='$emit(`update:modelValue`, $event.target.value === `` ? undefined : Number($event.target.value))'><slot /></select>"
  },
  "a-option": { template: "<option><slot /></option>" },
  "a-range-picker": { template: "<div data-testid='alarm-range' />" },
  "a-button": {
    emits: ["click"],
    template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
  },
  "a-table": {
    props: ["data", "pagination", "loading", "selectedKeys", "rowSelection"],
    emits: ["pageChange", "pageSizeChange", "update:selectedKeys"],
    provide() {
      return { alarmTable: this };
    },
    template:
      "<div data-testid='alarm-table' :data-count='data.length' :data-total='pagination.total' :data-ids='data.map(item => item.id).join(`,`)' :data-has-row-selection='Boolean(rowSelection)'><slot name='columns' /><button data-testid='page-three' @click='$emit(`pageChange`, 3)'>3</button><button data-testid='select-all' @click='$emit(`update:selectedKeys`, data.map(item => item.id))'>select</button></div>"
  },
  "a-table-column": {
    props: ["title"],
    inject: ["alarmTable"],
    template:
      "<span>{{ title }}<template v-for='record in alarmTable.data'><slot name='cell' :record='record' /></template></span>"
  },
  "a-link": { emits: ["click"], template: "<a @click='$emit(`click`)'><slot /></a>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-tooltip": { template: "<span><slot /></span>" },
  "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
  "a-alert": { template: "<div><slot /></div>" },
  AlarmDetailDrawer: {
    props: ["visible"],
    template: "<div data-testid='alarm-detail-drawer-stub' :data-visible='visible' />"
  }
};

function mountPage() {
  return mount(AlarmManagement, { global: { stubs } });
}

describe("AlarmManagement", () => {
  beforeEach(() => {
    account.permissions = ["gb28181:alarm:view"];
    alarmApi.listAlarms.mockReset();
    alarmApi.listAlarms.mockResolvedValue(listResult());
    alarmApi.deleteAlarm.mockReset();
    alarmApi.deleteAlarm.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { deletedIds: ["9007199254740993"], deletedCount: 1 }
    });
    alarmApi.batchDeleteAlarms.mockReset();
    alarmApi.batchDeleteAlarms.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { deletedIds: ["9007199254740993"], deletedCount: 1 }
    });
    alarmApi.clearAllAlarms.mockReset();
    alarmApi.clearAllAlarms.mockResolvedValue({ code: 0, message: "ok", data: { deletedCount: 41 } });
    modal.warning.mockReset();
    messages.error.mockReset();
    messages.success.mockReset();
    messages.warning.mockReset();
    deviceApi.listDevices.mockReset();
    deviceApi.listDevices.mockResolvedValue({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, pageSize: 20 } });
  });

  it("does not render the redundant page header", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find(".alarm-page-header").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("集中查看权限范围内的设备告警");
    expect(wrapper.find("[data-testid='alarm-refresh']").exists()).toBe(false);
  });

  it("uses one text search for device, channel and alarm description", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find("[data-testid='device-filter']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='source-code']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='keyword']").attributes("data-placeholder")).toBe(
      "设备 / 通道名称或国标编码 / 告警描述"
    );
    expect(deviceApi.listDevices).not.toHaveBeenCalled();
  });

  it("hides batch actions and row selection from view-only users", async () => {
    account.permissions = ["gb28181:alarm:view"];
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find(".alarm-batch-bar").exists()).toBe(false);
    expect(wrapper.find("[data-testid='batch-delete']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='alarm-clear-all']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-has-row-selection")).toBe("false");
  });

  it("shows row selection and a clear-all button for delete users", async () => {
    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-has-row-selection")).toBe("true");
    expect(wrapper.find("[data-testid='alarm-clear-all']").exists()).toBe(true);
  });

  it("batch deletes selected rows after confirmation", async () => {
    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get("[data-testid='select-all']").trigger("click");
    await flushPromises();
    expect(wrapper.find(".alarm-batch-bar").exists()).toBe(true);

    await wrapper.get("[data-testid='batch-delete']").trigger("click");
    expect(modal.warning).toHaveBeenCalledTimes(1);
    await modal.warning.mock.calls[0][0].onOk();
    await flushPromises();
    expect(alarmApi.batchDeleteAlarms).toHaveBeenCalledWith(["9007199254740993"]);
    expect(messages.success).toHaveBeenCalledWith("已物理删除 1 条告警");
  });

  it("clears all alarms after confirmation", async () => {
    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get("[data-testid='alarm-clear-all']").trigger("click");
    expect(modal.warning).toHaveBeenCalledTimes(1);
    await modal.warning.mock.calls[0][0].onOk();
    await flushPromises();
    expect(alarmApi.clearAllAlarms).toHaveBeenCalledTimes(1);
    expect(messages.success).toHaveBeenCalledWith("已清空 41 条告警");
  });

  it("loads the first page and renders the server total", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenCalledWith({ page: 1, pageSize: 10 });
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-count")).toBe("1");
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-total")).toBe("41");
    expect(wrapper.text()).toContain("平台接收时间");
    expect(wrapper.text()).toContain("设备告警时间");
  });

  it("links alarm type options to the selected alarm method", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const typeSelect = wrapper.get("[data-testid='alarm-type']");
    expect(typeSelect.attributes()).toHaveProperty("disabled");

    await wrapper.get("[data-testid='alarm-method']").setValue("5");
    await flushPromises();
    expect(typeSelect.attributes()).not.toHaveProperty("disabled");
    expect(typeSelect.attributes("data-placeholder")).toBe("告警类型");
    expect(typeSelect.text()).toContain("运动目标检测报警");
    expect(typeSelect.text()).not.toContain("类型 2");

    await wrapper.get("[data-testid='alarm-method']").setValue("2");
    await flushPromises();
    expect(typeSelect.text()).toContain("设备防拆报警");
    expect(typeSelect.text()).not.toContain("运动目标检测报警");
  });

  it("does not query when the account lacks view permission", async () => {
    account.permissions = ["unrelated"];
    const wrapper = mountPage();
    await flushPromises();
    expect(alarmApi.listAlarms).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("无权查看告警");
  });

  it("normalizes filters and resets to page one", async () => {
    const wrapper = mountPage();
    await flushPromises();
    alarmApi.listAlarms.mockClear();
    await wrapper.get("[data-testid='keyword']").setValue(" 移动 ");
    await wrapper.get("[data-testid='alarm-query']").trigger("click");
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 10,
      keyword: "移动"
    });
  });

  it("ignores a stale list response", async () => {
    let resolveOld!: (value: unknown) => void;
    alarmApi.listAlarms.mockImplementationOnce(() => new Promise(resolve => (resolveOld = resolve)));
    alarmApi.listAlarms.mockResolvedValueOnce(listResult("2"));
    const wrapper = mountPage();
    await wrapper.get("[data-testid='alarm-query']").trigger("click");
    await flushPromises();
    resolveOld(listResult("1"));
    await flushPromises();
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-ids")).toBe("2");
    expect(wrapper.text()).not.toContain("查询告警失败");
  });

  it("registers the alarm menu icon", () => {
    expect(getLucideIconComponent("lucide:BellRing")).toBeTruthy();
  });

  it("single delete requires permission, warns physical deletion and locks duplicate requests", async () => {
    const viewOnly = mountPage();
    await flushPromises();
    expect(viewOnly.find("[data-testid='single-delete-9007199254740993']").exists()).toBe(false);

    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    let resolveDelete!: (value: unknown) => void;
    alarmApi.deleteAlarm.mockImplementationOnce(() => new Promise(resolve => (resolveDelete = resolve)));
    const wrapper = mountPage();
    await flushPromises();
    const button = wrapper.get("[data-testid='single-delete-9007199254740993']");
    await button.trigger("click");
    expect(modal.warning).toHaveBeenCalledTimes(1);
    const options = modal.warning.mock.calls[0][0];
    expect(options.okText).toBe("删除");
    expect(String(options.content)).toContain("物理删除");
    expect(String(options.content)).toContain("不可恢复");
    expect(String(options.content)).toContain("一号机房");
    expect(String(options.content)).toContain("东门");
    expect(options.escToClose).toBe(true);

    const deleting = options.onOk();
    await button.trigger("click");
    expect(modal.warning).toHaveBeenCalledTimes(1);
    expect(alarmApi.deleteAlarm).toHaveBeenCalledTimes(1);
    resolveDelete({ code: 0, message: "ok", data: { deletedIds: ["9007199254740993"], deletedCount: 1 } });
    await deleting;
    await flushPromises();
    expect(alarmApi.deleteAlarm).toHaveBeenCalledWith("9007199254740993");
    expect(messages.success).toHaveBeenCalledWith("已物理删除 1 条告警");
  });

  it("opens alarm detail from the keyboard", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get(".uvp-table-action--detail").trigger("keydown", { key: "Enter" });
    expect(wrapper.get("[data-testid='alarm-detail-drawer-stub']").attributes("data-visible")).toBe("true");
  });

  it("keeps the row on delete failure and surfaces the backend message", async () => {
    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    alarmApi.deleteAlarm.mockRejectedValueOnce({ response: { data: { message: "告警已发生变化，未删除任何记录" } } });
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get("[data-testid='single-delete-9007199254740993']").trigger("click");
    await modal.warning.mock.calls[0][0].onOk();
    await flushPromises();
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-count")).toBe("1");
    expect(messages.error).toHaveBeenCalledWith("告警已发生变化，未删除任何记录");
    expect(alarmApi.listAlarms).toHaveBeenCalledTimes(1);
  });

  it("moves back before reloading when the last row on a later page is deleted", async () => {
    account.permissions = ["gb28181:alarm:view", "gb28181:alarm:delete"];
    alarmApi.listAlarms
      .mockResolvedValueOnce(listResult())
      .mockResolvedValueOnce({ ...listResult(), data: { ...listResult().data, page: 3, total: 21 } })
      .mockResolvedValueOnce({ ...listResult("40"), data: { ...listResult("40").data, page: 2, total: 20 } });
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get("[data-testid='page-three']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='single-delete-9007199254740993']").trigger("click");
    await modal.warning.mock.calls[0][0].onOk();
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenLastCalledWith({ page: 2, pageSize: 10 });
  });

});
