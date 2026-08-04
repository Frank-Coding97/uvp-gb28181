import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const alarmApi = vi.hoisted(() => ({ listAlarms: vi.fn() }));
const deviceApi = vi.hoisted(() => ({ listDevices: vi.fn() }));
const account = vi.hoisted(() => ({ permissions: ["gb28181:alarm:view"] as string[] }));

vi.mock("./api", async importOriginal => ({
  ...(await importOriginal<typeof import("./api")>()),
  ...alarmApi
}));
vi.mock("../device-mgmt/api", async importOriginal => ({
  ...(await importOriginal<typeof import("../device-mgmt/api")>()),
  ...deviceApi
}));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@/hooks/useGlobalProperties", () => ({
  default: () => ({ $message: { error: vi.fn(), warning: vi.fn(), success: vi.fn() } })
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
    pageSize: 20
  }
});

const stubs = {
  "s-layout-search": { template: "<section><slot name='fields' /><slot name='actions' /><slot name='extra' /></section>" },
  "a-input": {
    props: ["modelValue"],
    emits: ["update:modelValue", "pressEnter"],
    template:
      "<input :data-testid='$attrs[`data-testid`]' :value='modelValue' @input='$emit(`update:modelValue`, $event.target.value)' @keyup.enter='$emit(`pressEnter`)' />"
  },
  "a-select": {
    props: ["modelValue"],
    emits: ["update:modelValue", "search"],
    template: "<select :data-testid='$attrs[`data-testid`]' :value='modelValue' @change='$emit(`update:modelValue`, $event.target.value)'><slot /></select>"
  },
  "a-option": { template: "<option><slot /></option>" },
  "a-range-picker": { template: "<div data-testid='alarm-range' />" },
  "a-button": {
    emits: ["click"],
    template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
  },
  "a-table": {
    props: ["data", "pagination", "loading", "selectedKeys"],
    emits: ["pageChange", "pageSizeChange", "update:selectedKeys"],
    template:
      "<div data-testid='alarm-table' :data-count='data.length' :data-total='pagination.total' :data-ids='data.map(item => item.id).join(`,`)'><slot name='columns' /></div>"
  },
  "a-table-column": { props: ["title"], template: "<span>{{ title }}</span>" },
  "a-link": { template: "<a><slot /></a>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-tooltip": { template: "<span><slot /></span>" },
  "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
  "a-alert": { template: "<div><slot /></div>" }
};

function mountPage() {
  return mount(AlarmManagement, { global: { stubs } });
}

describe("AlarmManagement", () => {
  beforeEach(() => {
    account.permissions = ["gb28181:alarm:view"];
    alarmApi.listAlarms.mockReset();
    alarmApi.listAlarms.mockResolvedValue(listResult());
    deviceApi.listDevices.mockReset();
    deviceApi.listDevices.mockResolvedValue({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, pageSize: 20 } });
  });

  it("loads the first page and renders the server total", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenCalledWith({ page: 1, pageSize: 20 });
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-count")).toBe("1");
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-total")).toBe("41");
    expect(wrapper.text()).toContain("平台接收时间");
    expect(wrapper.text()).toContain("设备告警时间");
  });

  it("does not query when the account lacks view permission", async () => {
    account.permissions = ["unrelated"];
    const wrapper = mountPage();
    await flushPromises();
    expect(alarmApi.listAlarms).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("无权查看告警");
  });

  it("normalizes filters, resets to page one and keeps them on refresh", async () => {
    const wrapper = mountPage();
    await flushPromises();
    alarmApi.listAlarms.mockClear();
    await wrapper.get("[data-testid='source-code']").setValue(" 3701 ");
    await wrapper.get("[data-testid='keyword']").setValue(" 移动 ");
    await wrapper.get("[data-testid='alarm-query']").trigger("click");
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 20,
      sourceCode: "3701",
      keyword: "移动"
    });
    await wrapper.get("[data-testid='alarm-refresh']").trigger("click");
    await flushPromises();
    expect(alarmApi.listAlarms).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 20,
      sourceCode: "3701",
      keyword: "移动"
    });
  });

  it("ignores a stale list response", async () => {
    let resolveOld!: (value: unknown) => void;
    alarmApi.listAlarms.mockImplementationOnce(() => new Promise(resolve => (resolveOld = resolve)));
    alarmApi.listAlarms.mockResolvedValueOnce(listResult("2"));
    const wrapper = mountPage();
    await wrapper.get("[data-testid='alarm-refresh']").trigger("click");
    await flushPromises();
    resolveOld(listResult("1"));
    await flushPromises();
    expect(wrapper.get("[data-testid='alarm-table']").attributes("data-ids")).toBe("2");
    expect(wrapper.text()).not.toContain("查询告警失败");
  });

  it("registers the alarm menu icon", () => {
    expect(getLucideIconComponent("lucide:BellRing")).toBeTruthy();
  });
});
