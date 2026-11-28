import { flushPromises, shallowMount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import NodeListPanel from "./NodeListPanel.vue";

const api = vi.hoisted(() => ({ enable: vi.fn(), disable: vi.fn(), list: vi.fn() }));
const feedback = vi.hoisted(() => ({ confirm: vi.fn(), success: vi.fn(), error: vi.fn(), warning: vi.fn() }));
vi.mock("@/api/gb28181-zlm", () => ({
  enableZLMNode: api.enable,
  disableZLMNode: api.disable,
  listZLMNodes: api.list
}));
vi.mock("@arco-design/web-vue", () => ({
  Message: { success: feedback.success, error: feedback.error, warning: feedback.warning },
  Modal: { confirm: feedback.confirm }
}));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => ({ push: vi.fn() })
}));
vi.mock("../../NodeForm.vue", () => ({ default: { template: "<div />" } }));
vi.mock("../../ZLMNodeActionDialog.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/hooks/usePermission", () => ({ hasPermission: () => true }));
vi.mock("@/hooks/useDictOptions", () => ({ useDictOptions: () => [] }));

const wrappers: ReturnType<typeof shallowMount>[] = [];
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()));
beforeEach(() => {
  vi.clearAllMocks();
  api.enable.mockResolvedValue({ code: 0, data: { ok: true, enabled: true, fixedAddressPlaybackDisabled: true } });
});

function mountNode(mode: "single" | "multi") {
  const node = {
    id: 7,
    name: "北京节点",
    host: "192.0.2.1",
    state: "active",
    enabled: false,
    rtpReceiveMode: mode,
    stats: {},
    autoOnDemandReady: true
  };
  const wrapper = shallowMount(NodeListPanel, { props: { nodes: [node] as any, autoRefresh: false } });
  wrappers.push(wrapper);
  return { wrapper, node };
}

describe("single-port node enable confirmation", () => {
  it("explains the global playback effect before enabling a single-port node", async () => {
    const { wrapper, node } = mountNode("single");
    (wrapper.vm as any).handleEnabled(node);
    expect(feedback.confirm).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "启用单端口媒体节点",
        content: expect.stringContaining("关闭全局固定播放地址及自动点播")
      })
    );
    expect(api.enable).not.toHaveBeenCalled();
    await feedback.confirm.mock.calls[0][0].onOk();
    await flushPromises();
    expect(api.enable).toHaveBeenCalledWith(7);
    expect(feedback.success).toHaveBeenCalledWith(expect.stringContaining("已关闭固定播放地址及自动点播"));
  });

  it("preserves one-click enable for multi-port nodes", async () => {
    const { wrapper, node } = mountNode("multi");
    api.enable.mockResolvedValue({ code: 0, data: { ok: true, enabled: true } });
    (wrapper.vm as any).handleEnabled(node);
    await flushPromises();
    expect(feedback.confirm).not.toHaveBeenCalled();
    expect(api.enable).toHaveBeenCalledWith(7);
    expect(feedback.success).toHaveBeenCalledWith("节点已启用，可参与新任务调度");
  });

  it("does not report linked changes when enabling fails", async () => {
    const { wrapper, node } = mountNode("single");
    api.enable.mockResolvedValue({ code: 500, message: "配置保存失败" });
    (wrapper.vm as any).handleEnabled(node);
    await feedback.confirm.mock.calls[0][0].onOk();
    await flushPromises();
    expect(feedback.error).toHaveBeenCalled();
    expect(feedback.success).not.toHaveBeenCalled();
  });
});
