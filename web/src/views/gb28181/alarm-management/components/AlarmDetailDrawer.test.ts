import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ getAlarmDetail: vi.fn() }));
vi.mock("../api", async importOriginal => ({
  ...(await importOriginal<typeof import("../api")>()),
  ...api
}));

import AlarmDetailDrawer from "./AlarmDetailDrawer.vue";

const detail = (id = "9007199254740993", description = "移动目标") => ({
  id,
  receivedAt: "2026-08-04T17:49:11+08:00",
  alarmTime: "2026-08-04T17:48:00+08:00",
  device: { id: 12, code: "37010301021320000111", name: "NVR", alias: "一号机房" },
  channel: { id: 31, code: "37010301021320000112", name: "通道1", alias: "东门" },
  sourceCode: "37010301021320000112",
  priority: { value: 1, label: "一级警情" },
  method: { value: 5, label: "视频报警" },
  alarmType: { value: 2, label: "运动目标检测报警" },
  description,
  alarmTypeParam: "区域=东门",
  longitude: 116.397,
  latitude: 39.908,
  rawDigest: "sha256:abc",
  rawSummary: "<Notify><Description>有人 & 移动</Description></Notify>",
  createdAt: "2026-08-04T17:49:12+08:00",
  updatedAt: "2026-08-04T17:49:12+08:00"
});

const stubs = {
  "a-drawer": {
    props: ["visible", "escToClose"],
    template:
      "<aside v-if='visible' data-testid='alarm-detail-drawer' :data-esc-to-close='escToClose'><slot name='title' /><slot /></aside>"
  },
  "a-spin": { props: ["loading"], template: "<div :data-loading='loading'><slot /></div>" },
  "a-descriptions": { template: "<dl><slot /></dl>" },
  "a-descriptions-item": { props: ["label"], template: "<div><dt>{{ label }}</dt><dd><slot /></dd></div>" },
  "a-alert": { template: "<div><slot /></div>" },
  // ⛔ 2026-11-04 起删除改成 a-popconfirm 气泡确认，点一下按钮**不再**等于删除：
  //    必须再点气泡里的「删除」。这个 stub 把气泡的 ok 按钮暴露成 `popconfirm-ok`，
  //    用来钉住「按钮 → 气泡 → 确认」这条链路，同时验证 disabled 会转发到气泡上。
  "a-popconfirm": {
    emits: ["ok"],
    props: ["disabled", "okText", "cancelText", "okButtonProps"],
    template:
      "<span><slot /><button data-testid='popconfirm-ok' :disabled='disabled' @click='$emit(`ok`)'>{{ okText }}</button></span>"
  },
  "a-button": {
    emits: ["click"],
    template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot /></button>"
  },
  "a-tag": { template: "<span><slot /></span>" }
};

function mountDrawer(id = "9007199254740993") {
  return mount(AlarmDetailDrawer, {
    props: { visible: true, alarmId: id, canDelete: false },
    global: { stubs }
  });
}

describe("AlarmDetailDrawer", () => {
  beforeEach(() => {
    api.getAlarmDetail.mockReset();
    api.getAlarmDetail.mockResolvedValue({ code: 0, message: "ok", data: detail() });
  });

  it("preserves a bigint string ID and renders complete detail fields", async () => {
    const wrapper = mountDrawer();
    await flushPromises();
    expect(api.getAlarmDetail).toHaveBeenCalledWith("9007199254740993");
    expect(wrapper.text()).toContain("区域=东门");
    expect(wrapper.text()).toContain("116.397");
    expect(wrapper.text()).toContain("39.908");
    expect(wrapper.text()).toContain("sha256:abc");
    expect(wrapper.text()).toContain("<Notify><Description>有人 & 移动</Description></Notify>");
    expect(wrapper.find("script").exists()).toBe(false);
    // ⭐ 权限改造（2026-10-11）：无删除权限时按钮保留可见但置灰。
    expect(wrapper.get("[data-testid='detail-delete']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-testid='alarm-detail-drawer']").attributes("data-esc-to-close")).toBe("true");
  });

  it("clears the previous detail while a new alarm is loading", async () => {
    api.getAlarmDetail.mockResolvedValueOnce({ code: 0, message: "ok", data: detail("1", "旧告警") });
    let resolveNew!: (value: unknown) => void;
    api.getAlarmDetail.mockImplementationOnce(() => new Promise(resolve => (resolveNew = resolve)));
    const wrapper = mountDrawer("1");
    await flushPromises();
    expect(wrapper.text()).toContain("旧告警");
    await wrapper.setProps({ alarmId: "2" });
    expect(wrapper.text()).not.toContain("旧告警");
    resolveNew({ code: 0, message: "ok", data: detail("2", "新告警") });
    await flushPromises();
    expect(wrapper.text()).toContain("新告警");
  });

  it("shows an isolated failure state and supports retry", async () => {
    api.getAlarmDetail.mockRejectedValueOnce(new Error("404"));
    const wrapper = mountDrawer("404");
    await flushPromises();
    expect(wrapper.text()).toContain("加载告警详情失败");
    api.getAlarmDetail.mockResolvedValueOnce({ code: 0, message: "ok", data: detail("404") });
    await wrapper.get("[data-testid='detail-retry']").trigger("click");
    await flushPromises();
    expect(api.getAlarmDetail).toHaveBeenLastCalledWith("404");
    expect(wrapper.text()).toContain("sha256:abc");
  });

  it("ignores an older response after switching alarms", async () => {
    let resolveOld!: (value: unknown) => void;
    api.getAlarmDetail.mockImplementationOnce(() => new Promise(resolve => (resolveOld = resolve)));
    api.getAlarmDetail.mockResolvedValueOnce({ code: 0, message: "ok", data: detail("2", "第二条") });
    const wrapper = mountDrawer("1");
    await wrapper.setProps({ alarmId: "2" });
    await flushPromises();
    resolveOld({ code: 0, message: "ok", data: detail("1", "第一条") });
    await flushPromises();
    expect(wrapper.text()).toContain("第二条");
    expect(wrapper.text()).not.toContain("第一条");
  });

  it("emits the same single-delete intent only after the confirm bubble is accepted", async () => {
    const withoutPermission = mountDrawer();
    await flushPromises();
    // ⭐ 权限改造（2026-10-11）：无删除权限时按钮保留可见但置灰。
    //   2026-11-04 起删除走气泡，disabled 同时转发到气泡本身（不可打开），这里一并钉住。
    expect(withoutPermission.get("[data-testid='detail-delete']").attributes("disabled")).toBeDefined();
    expect(withoutPermission.get("[data-testid='popconfirm-ok']").attributes("disabled")).toBeDefined();

    const wrapper = mount(AlarmDetailDrawer, {
      props: { visible: true, alarmId: "9007199254740993", canDelete: true },
      global: { stubs }
    });
    await flushPromises();
    // 点触发按钮本身不应产生删除意图（否则等于没确认）
    await wrapper.get("[data-testid='detail-delete']").trigger("click");
    expect(wrapper.emitted("delete")).toBeUndefined();

    await wrapper.get("[data-testid='popconfirm-ok']").trigger("click");
    expect(wrapper.emitted("delete")?.[0]?.[0]).toEqual(detail());
  });
});
