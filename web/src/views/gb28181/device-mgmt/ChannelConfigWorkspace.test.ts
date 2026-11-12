import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { Modal } from "@arco-design/web-vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChannelConfigWorkspace from "./ChannelConfigWorkspace.vue";

const api = vi.hoisted(() => ({
  getChannelVideoParams: vi.fn(),
  applyChannelVideoParams: vi.fn(),
  getChannelDeviceConfigs: vi.fn(),
  applyChannelDeviceConfigs: vi.fn(),
  createDeviceSnapshotSession: vi.fn(),
  getDeviceSnapshotSession: vi.fn()
}));
vi.mock("@/api/gb28181", () => api);

function mountWorkspace(props = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/", component: { template: "<div/>" } }] });
  return mount(ChannelConfigWorkspace, {
    props: {
      channelId: 1,
      channelName: "后置摄像头",
      deviceCode: "device-1",
      online: true,
      channelOptions: [{ value: 1, label: "后置摄像头", online: true }],
      canRead: true,
      canApply: true,
      canSnapshot: true,
      canFormat: true,
      ...props
    },
    global: { plugins: [createPinia(), router], stubs: { DeviceStatusFactsPanel: true, StorageCardStatusPanel: true } }
  });
}

describe("通道配置工作区", () => {
  beforeEach(() => {
    vi.stubGlobal("ref", ref);
    api.getChannelDeviceConfigs.mockReset().mockResolvedValue({
      code: 0,
      data: { list: [{ configType: "BasicParam", payload: { name: "原名称" } }], reconcile: { state: "read_ok" } }
    });
  });

  it("只有一套功能导航，切换后保留配置草稿", async () => {
    const wrapper = mountWorkspace();
    await flushPromises();
    expect(wrapper.findAll(".channel-workspace-link")).toHaveLength(7);
    await wrapper.get("[data-testid='channel-nav-basic']").trigger("click");
    await wrapper.get("[data-testid='dcg-field-name'] input").setValue("新的名称");
    await wrapper.get("[data-testid='channel-nav-status']").trigger("click");
    expect((wrapper.vm as any).dirtyCount).toBe(1);
    await wrapper.get("[data-testid='channel-nav-basic']").trigger("click");
    expect((wrapper.get("[data-testid='dcg-field-name'] input").element as HTMLInputElement).value).toBe("新的名称");
    expect(wrapper.find(".dcg-nav").exists()).toBe(false);
    wrapper.unmount();
  });

  it("关闭、换通道共用同一个确认，取消保留草稿，放弃才允许离开", async () => {
    const confirm = vi.spyOn(Modal, "confirm").mockReturnValue({} as any);
    const wrapper = mountWorkspace();
    await flushPromises();
    await wrapper.get("[data-testid='channel-nav-basic']").trigger("click");
    await wrapper.get("[data-testid='dcg-field-name'] input").setValue("新的名称");
    const first = (wrapper.vm as any).beforeLeave();
    const duplicate = (wrapper.vm as any).beforeLeave();
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(duplicate).toBe(first);
    (confirm.mock.calls[0][0] as any).onCancel?.();
    expect(await first).toBe(false);
    expect((wrapper.vm as any).dirtyCount).toBe(1);
    const second = (wrapper.vm as any).beforeLeave();
    await (confirm.mock.calls[1][0] as any).onOk?.();
    expect(await second).toBe(true);
    wrapper.unmount();
  });

  it("抓拍接收结果在配置和抓拍之间切换后仍保留", async () => {
    const session = { sessionId: "snap-1", state: "completed", receivedCount: 1, snapNum: 1, files: [] };
    api.createDeviceSnapshotSession.mockResolvedValue({ code: 0, data: session });
    api.getDeviceSnapshotSession.mockResolvedValue({ code: 0, data: session });
    const wrapper = mountWorkspace();
    await flushPromises();
    await wrapper.get("[data-testid='channel-nav-snapshot']").trigger("click");
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='channel-nav-record-plan']").trigger("click");
    await wrapper.get("[data-testid='channel-nav-snapshot']").trigger("click");
    expect(wrapper.get("[data-testid='snapshot-status']").text()).toBe("已完成 1/1");
    expect(api.createDeviceSnapshotSession).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("只有抓拍权限时直接显示抓拍，配置与事实查询禁用", async () => {
    api.getChannelDeviceConfigs.mockClear();
    const wrapper = mountWorkspace({ canRead: false, canApply: false });
    await flushPromises();
    expect(wrapper.get("[data-testid='channel-nav-snapshot']").attributes("aria-current")).toBe("page");
    expect(wrapper.get("[data-testid='channel-nav-record-plan']").attributes("disabled")).toBeDefined();
    expect(api.getChannelDeviceConfigs).not.toHaveBeenCalled();
    expect(await (wrapper.vm as any).beforeLeave()).toBe(true);
    wrapper.unmount();
  });

  /**
   * 底栏必须贴底（2026-11-03 按钮专题）。
   *
   * ⛔⛔ 高度链条断在 `.dcg-host--embedded` 上：它是 `display:block` 且 height 是 auto，
   *    而里面的 `.dcg-window--embedded` 写的是 `height:100%` —— 百分比拿 auto 算
   *    ⇒ 退化成 auto ⇒ 面板塌成"内容多高就多高"，`.dcg-actions` 停在内文下面，
   *    底下一大片空白（截图里下发键离窗口底边还有 400px）。
   * ⇒ 编辑区必须是 flex 列，且把剩余高度交给 `.dcg-host`。
   *   happy-dom 不算布局，所以这条只能断源码结构，真机效果靠这份结构兜住。
   */
  it("编辑区把确定高度传进配置面板，底部操作条才贴得住窗口底边", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/ChannelConfigWorkspace.vue"), "utf8");
    const editor = source.match(/\.channel-workspace-editor\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(editor).toContain("display: flex");
    expect(editor).toContain("flex-direction: column");
    expect(editor).toContain("flex: 1");
    expect(editor).toContain("min-height: 0");

    const host = source.match(/\.channel-workspace-editor :deep\(\.dcg-host\)\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(host, "少了给 .dcg-host 高度的规则").toContain("flex: 1 1 auto");
    expect(host).toContain("min-height: 0");
  });
});
