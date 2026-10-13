import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RecordCacheTask } from "@/api/recordCache";
import RecordCacheProgressDialog from "./RecordCacheProgressDialog.vue";

const api = vi.hoisted(() => ({
  getRecordCacheTask: vi.fn(),
  cancelRecordCacheTask: vi.fn()
}));

const messages = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }));

vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  ...api
}));

vi.mock("@arco-design/web-vue", () => ({
  Message: { success: messages.success, error: messages.error, info: messages.info }
}));

const stubs = {
  "a-modal": {
    // ⛔ 必须捕获 `modalClass`：它决定暗色下用哪套面板皮肤（见下面那条用例）。
    props: ["visible", "footer", "title", "width", "unmountOnClose", "modalClass"],
    emits: ["cancel"],
    template: "<div v-if='visible' data-testid='cache-modal' :data-modal-class='modalClass'><slot /></div>"
  },
  "a-progress": {
    props: ["percent", "status", "showText", "size"],
    template: "<div class='dialog-progress' :data-percent='percent' :data-status='status' />"
  },
  "a-button": {
    props: ["loading", "disabled", "type", "size"],
    emits: ["click"],
    template: "<button :disabled='disabled' @click='$emit(`click`)'><slot /></button>"
  }
};

function task(overrides: Partial<RecordCacheTask> = {}): RecordCacheTask {
  return {
    taskId: "t1",
    channelId: 31,
    deviceId: "34020000001320000001",
    channelCode: "34020000001320000001",
    channelName: "东门出入口",
    deviceName: "一号 NVR",
    startTime: "2026-08-02T08:10:00+08:00",
    endTime: "2026-08-02T08:42:16+08:00",
    recordType: "time",
    downloadSpeed: 4,
    state: "running",
    lastError: "",
    cachedBytes: 10485760,
    cachedSeconds: 300,
    totalSeconds: 1936,
    progress: 0.15,
    speedBytesPerSec: 2097152,
    estimatedBytes: 0,
    favorite: false,
    files: [],
    createdByName: "admin",
    createdAt: "2026-10-03T16:00:00+08:00",
    startedAt: "2026-10-03T16:00:05+08:00",
    finishedAt: "",
    expiresAt: "2026-10-10T16:00:00+08:00",
    ...overrides
  };
}

function mountDialog(props: { visible: boolean; taskId: string; taskName?: string } = { visible: true, taskId: "t1" }) {
  return mount(RecordCacheProgressDialog, { props, global: { stubs } });
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const target = wrapper.findAll("button").find(candidate => candidate.text().trim() === text);
  if (!target) throw new Error(`没有找到文案为「${text}」的按钮`);
  return target;
}

enableAutoUnmount(afterEach);

describe("record cache progress dialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task() });
    api.cancelRecordCacheTask.mockResolvedValue({ code: 0, data: task({ state: "cancelled" }) });
  });

  it("打开时立刻拉一次任务并展示进度、时长与实时速率", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    expect(api.getRecordCacheTask).toHaveBeenCalledWith("t1", { showErrorMessage: false });
    // ⛔ 喂给 `a-progress` 的必须是 0~1 的比值：Arco 的 `width = percent * 100%`，
    //    传百分数 15 会渲染成 width:1500%（被裁成满格），右侧文字却还是 15%。
    expect(wrapper.get(".dialog-progress").attributes("data-percent")).toBe("0.15");
    // 文字仍必须是百分数。
    expect(wrapper.get("[data-testid='record-cache-percent']").text()).toBe("15%");
    const markup = wrapper.text();
    expect(markup).toContain("正在从设备拉流并缓存到服务器");
    expect(markup).toContain("5分0秒 / 32分16秒");
    expect(markup).toContain("10.0 MB");
    expect(markup).toContain("2.0 MB/s");
  });

  it("弹窗挂上系统统一的面板类 —— 漏了它暗色下会退回 Arco 自带的灰面板", async () => {
    // ⛔ `uvp-system-dialog` 是本仓弹窗面板底色/描边/阴影/页脚按钮的**唯一来源**
    //    （uvp-ui-language.scss 里 `.uvp-system-dialog .arco-modal-*` 那一组）。
    //    漏掉它不会报任何错、界面照常打开，只是深色主题下它跟旁边每一个弹窗都不是一套皮肤
    //    —— 正是 2026-10-05 用户报的"配色和 UI 不统一"。
    const wrapper = mountDialog();
    await flushPromises();
    expect(wrapper.get("[data-testid='cache-modal']").attributes("data-modal-class")).toContain("uvp-system-dialog");
  });

  it("未完成任务会挂上 2 秒后的下一次轮询", async () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    mountDialog();
    await flushPromises();
    expect(setTimeoutSpy).toHaveBeenCalledWith(expect.any(Function), 2000);
  });

  it("任务进入终态后不再轮询，并提示去列表下载", async () => {
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ state: "succeeded", progress: 1, speedBytesPerSec: 0 })
    });
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const wrapper = mountDialog();
    await flushPromises();

    expect(setTimeoutSpy).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("缓存完成，可下载到本地");
    // 已完成时进度条必须钉在 100%，不能沿用最后一次上报的 0.15。
    expect(wrapper.get(".dialog-progress").attributes("data-percent")).toBe("1");
    expect(wrapper.get("[data-testid='record-cache-percent']").text()).toBe("100%");
  });

  it("失败任务展示后端给出的原因", async () => {
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ state: "failed", lastError: "设备未响应下载请求" })
    });
    const wrapper = mountDialog();
    await flushPromises();
    expect(wrapper.text()).toContain("设备未响应下载请求");
  });

  it("整理中：继续说清在干什么、继续轮询，但不给「取消任务」", async () => {
    // ⛔ 整理中（merging）录像已经全部拉完，取消没有意义，后端会回 409「任务已结束」。
    //    但轮询**必须继续** —— 不轮询的话弹窗会永远停在"整理中"，
    //    用户看不到"缓存完成，可下载到本地"那一步。
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ state: "merging", progress: 1, speedBytesPerSec: 0 })
    });
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const wrapper = mountDialog();
    await flushPromises();

    expect(wrapper.text()).toContain("正在整理成单个文件");
    expect(setTimeoutSpy).toHaveBeenCalledWith(expect.any(Function), 2000);
    const labels = wrapper.findAll("button").map(node => node.text().trim());
    expect(labels).not.toContain("取消任务");
    // 「转后台」必须还在：任务跑在服务端，整理阶段同样不需要用户干等。
    expect(labels).toContain("转后台");
  });

  it("取消成功后直接关闭弹窗，而不是留在原地露出「查看缓存任务」", async () => {
    // ⛔ 2026-10-05 用户报的 bug：取消完之后弹窗不关，底部的「取消任务」和「转后台」
    //    两个按钮因为状态已终态同时消失，兜底的 `v-else` 就把「查看缓存任务」顶了上来。
    //    取消的正确收尾只有一件事：关窗。台账和已落盘分片由后端保留。
    const wrapper = mountDialog();
    await flushPromises();

    await buttonByText(wrapper, "取消任务").trigger("click");
    await flushPromises();

    expect(api.cancelRecordCacheTask).toHaveBeenCalledWith("t1");
    expect(messages.success).toHaveBeenCalledWith("已取消缓存任务");
    expect(wrapper.emitted("update:visible")).toEqual([[false]]);
    // ⛔ 关窗 ≠ 跳走：取消不该把人送到录像缓存列表页（那是「查看缓存任务」的事）。
    expect(wrapper.emitted("navigate")).toBeUndefined();
  });

  it("取消失败时保持弹窗打开，用户还能再点一次", async () => {
    // ⛔ 失败的取消**不能**关窗：关掉之后用户既看不到失败原因、
    //    也再也够不着那个按钮，只能刷新页面重来。
    api.cancelRecordCacheTask.mockRejectedValue(new Error("cancelled already"));
    const wrapper = mountDialog();
    await flushPromises();

    await buttonByText(wrapper, "取消任务").trigger("click");
    await flushPromises();

    expect(messages.error).toHaveBeenCalledWith("取消失败");
    expect(wrapper.emitted("update:visible")).toBeUndefined();
  });

  it("转后台只关弹窗、不跳走、不取消任务", async () => {
    // ⛔ 2026-10-05 用户要求：点「转后台」不要再把人送到录像缓存页，
    //    直接关闭 dialog 就行（任务在服务端跑着，顶栏「缓存任务」能看到进度）。
    const wrapper = mountDialog();
    await flushPromises();

    await buttonByText(wrapper, "转后台").trigger("click");
    await flushPromises();

    expect(wrapper.emitted("update:visible")).toEqual([[false]]);
    // ⛔ 关键：不能发 navigate —— 那正是"跳走"这个行为的开关。
    expect(wrapper.emitted("navigate")).toBeUndefined();
    expect(messages.info).toHaveBeenCalled();
    // 转后台 ≠ 取消：不能顺手把服务端任务停掉。
    expect(api.cancelRecordCacheTask).not.toHaveBeenCalled();
  });

  it("「查看缓存任务」才关弹窗并跳转到列表页", async () => {
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ state: "succeeded", progress: 1, speedBytesPerSec: 0 })
    });
    const wrapper = mountDialog();
    await flushPromises();

    await buttonByText(wrapper, "查看缓存任务").trigger("click");
    await flushPromises();

    expect(wrapper.emitted("update:visible")).toEqual([[false]]);
    expect(wrapper.emitted("navigate")).toHaveLength(1);
    expect(api.cancelRecordCacheTask).not.toHaveBeenCalled();
  });

  it("隐藏时不拉取任务", async () => {
    mountDialog({ visible: false, taskId: "t1" });
    await flushPromises();
    expect(api.getRecordCacheTask).not.toHaveBeenCalled();
  });
});
