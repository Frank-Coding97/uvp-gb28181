import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { reactive } from "vue";

const coordinator = vi.hoisted(() => ({ cancel: vi.fn(), retry: vi.fn(), cancelAll: vi.fn(), refreshAll: vi.fn() }));
vi.mock("@/views/gb28181/cloud-recordings/recordingDownloadService", () => ({ recordingDownloadCoordinator: coordinator }));
const accountState = vi.hoisted(() => ({ permissions: ["gb28181:recording:download"] as string[] }));
const account = reactive(accountState);
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));

import { useRecordingDownloadStore } from "@/store/modules/recording-downloads";
import RecordingDownloadCenter from "./RecordingDownloadCenter.vue";

const stubs = {
  "a-doption": { emits: ["click"], template: "<button class='menu-option' @click='$emit(`click`)'><slot /></button>" },
  "a-badge": { template: "<span><slot /></span>" },
  "a-tooltip": { template: "<span><slot /></span>" },
  "a-button": {
    emits: ["click"],
    template:
      "<button :aria-expanded='$attrs[`aria-expanded`]' :aria-label='$attrs[`aria-label`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
  },
  "a-drawer": {
    props: ["visible"],
    emits: ["update:visible"],
    // ⛔ 捕获 class：抽屉面板的暗色皮肤全靠 `uvp-system-drawer` 这个类（见下面那条断言）。
    template: "<aside v-if='visible' :data-drawer-class='$attrs.class'><slot name='title' /><slot /></aside>"
  },
  "a-empty": { props: ["description"], template: "<span>{{ description }}</span>" },
  // ⛔ 捕获 `percent`：Arco 的 `a-progress` 收的是 0~1 的比值（`width = percent * 100%`），
  //    传百分数会被裁成"永远顶满"。这个 stub 就是这条口径的守卫。
  "a-progress": { props: ["percent"], template: "<span class='dl-progress' :data-percent='percent' />" }
};

describe("RecordingDownloadCenter", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    account.permissions = ["gb28181:recording:download"];
    coordinator.cancel.mockReset();
    coordinator.retry.mockReset();
    coordinator.cancelAll.mockReset();
    coordinator.refreshAll.mockReset();
  });

  it("hides the download center for users without recording download permission", () => {
    account.permissions = [];

    const headerWrapper = mount(RecordingDownloadCenter, { global: { stubs } });
    const menuWrapper = mount(RecordingDownloadCenter, { props: { menu: true }, global: { stubs } });

    expect(headerWrapper.find("button[aria-label='下载任务']").exists()).toBe(false);
    expect(menuWrapper.find(".menu-option").exists()).toBe(false);
    expect(coordinator.cancelAll).not.toHaveBeenCalled();
    expect(coordinator.refreshAll).not.toHaveBeenCalled();
  });

  it("accepts the administrator wildcard permission", () => {
    account.permissions = ["*:*:*"];

    const wrapper = mount(RecordingDownloadCenter, { global: { stubs } });

    expect(wrapper.find("button[aria-label='下载任务']").exists()).toBe(true);
  });

  it("rechecks download permission before mutating a task after access is revoked", async () => {
    const store = useRecordingDownloadStore();
    store.upsert({
      taskId: "one",
      fileId: "file-1",
      fileName: "one.mp4",
      status: "streaming",
      bytesSent: 50,
      totalBytes: 100,
      createdAt: "now",
      expiresAt: "later"
    });
    store.upsert({
      taskId: "two",
      fileId: "file-2",
      fileName: "two.mp4",
      status: "failed",
      bytesSent: 0,
      createdAt: "now",
      expiresAt: "later"
    });
    const wrapper = mount(RecordingDownloadCenter, { global: { stubs } });
    await wrapper.get("button[aria-label='下载任务']").trigger("click");
    const cancelButton = wrapper.get("button[aria-label='取消下载']");
    const retryButton = wrapper.get("button[aria-label='重新下载']");

    account.permissions = [];
    await cancelButton.trigger("click");
    await retryButton.trigger("click");

    expect(coordinator.cancel).not.toHaveBeenCalled();
    expect(coordinator.retry).not.toHaveBeenCalled();
  });

  it("shows in-memory task progress and delegates cancel/retry without exposing URLs", async () => {
    const store = useRecordingDownloadStore();
    store.upsert({
      taskId: "one",
      fileId: "file-1",
      fileName: "one.mp4",
      status: "streaming",
      bytesSent: 50,
      totalBytes: 100,
      createdAt: "now",
      expiresAt: "later"
    });
    store.upsert({
      taskId: "two",
      fileId: "file-2",
      fileName: "two.mp4",
      status: "failed",
      bytesSent: 0,
      createdAt: "now",
      expiresAt: "later"
    });
    const wrapper = mount(RecordingDownloadCenter, { global: { stubs } });
    const trigger = wrapper.get("button[aria-label='下载任务']");
    expect(trigger.attributes("aria-expanded")).toBe("false");
    await trigger.trigger("click");
    expect(wrapper.text()).toContain("one.mp4");
    // ⛔ 抽屉要挂系统统一的类：暗色下抽屉面板的底色/描边/圆角全由
    //    `.uvp-system-drawer .arco-drawer` 提供，漏了就退回 Arco 自带的灰面板，
    //    和旁边每一个弹层都不是一套皮肤（漏了不报错，只是难看）。
    expect(wrapper.get("aside").attributes("data-drawer-class")).toContain("uvp-system-drawer");
    expect(wrapper.text()).toContain("50%");
    // 条本身拿的是比值 0.5（= 50%），不是 50：传 50 会被 Arco 渲染成 width:5000%。
    expect(wrapper.get(".dl-progress").attributes("data-percent")).toBe("0.5");
    expect(wrapper.text()).not.toContain("contentUrl");
    await wrapper.get("button[aria-label='取消下载']").trigger("click");
    await wrapper.get("button[aria-label='重新下载']").trigger("click");
    expect(coordinator.cancel).toHaveBeenCalledWith("one");
    expect(coordinator.retry).toHaveBeenCalledWith("two");
  });

  it("shows known speed and ETA without inventing an ETA when they are unavailable", async () => {
    const store = useRecordingDownloadStore();
    store.upsert({
      taskId: "one",
      fileId: "file-1",
      fileName: "one.mp4",
      status: "streaming",
      bytesSent: 50,
      totalBytes: 100,
      speedBytesPerSecond: 25,
      createdAt: "now",
      expiresAt: "later"
    });
    store.upsert({
      taskId: "two",
      fileId: "file-2",
      fileName: "two.mp4",
      status: "streaming",
      bytesSent: 50,
      totalBytes: 100,
      createdAt: "now",
      expiresAt: "later"
    });
    store.upsert({
      taskId: "three",
      fileId: "file-3",
      fileName: "three.mp4",
      status: "streaming",
      bytesSent: 50,
      speedBytesPerSecond: 25,
      createdAt: "now",
      expiresAt: "later"
    });
    const wrapper = mount(RecordingDownloadCenter, { global: { stubs } });
    await wrapper.get("button[aria-label='下载任务']").trigger("click");
    const items = wrapper.findAll(".recording-download-item");
    expect(items[2].text()).toContain("25 B/s · 剩余2秒");
    expect(items[0].text()).toContain("25 B/s");
    expect(items[0].text()).not.toContain("剩余");
    expect(items[1].text()).not.toContain("剩余");
  });

  it("renders as an account-menu action without exposing the header button", async () => {
    const store = useRecordingDownloadStore();
    store.upsert({
      taskId: "one",
      fileId: "file-1",
      fileName: "one.mp4",
      status: "streaming",
      bytesSent: 50,
      totalBytes: 100,
      createdAt: "now",
      expiresAt: "later"
    });
    const wrapper = mount(RecordingDownloadCenter, { props: { menu: true }, global: { stubs } });

    expect(wrapper.find("button[aria-label='下载任务']").exists()).toBe(false);
    expect(wrapper.get(".menu-option").text()).toContain("下载任务");
    await wrapper.get(".menu-option").trigger("click");
    expect(wrapper.text()).toContain("one.mp4");
  });
});
