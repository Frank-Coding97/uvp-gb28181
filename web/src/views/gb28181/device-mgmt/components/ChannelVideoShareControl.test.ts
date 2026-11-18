import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChannelVideoShareControl from "./ChannelVideoShareControl.vue";
import type { ChannelVideoShareState } from "@/api/gb28181";

const shareApi = vi.hoisted(() => ({
  acquireChannelVideoShareLink: vi.fn(),
  getChannelVideoShare: vi.fn(),
  setChannelVideoShare: vi.fn()
}));
const modalConfirm = vi.hoisted(() => vi.fn());
const message = vi.hoisted(() => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn() }));
vi.mock("@/components/s-qrcode-draw/index.vue", () => ({ default: { template: "<img />" } }));
vi.mock("@/api/gb28181", () => shareApi);
vi.mock("@arco-design/web-vue", () => ({ Modal: { confirm: modalConfirm }, Message: message }));

function mountControl(mode: "switch" | "action", initialState?: ChannelVideoShareState) {
  return mount(ChannelVideoShareControl, {
    props: { channelId: 42, channelName: "门岗摄像头", initialState, mode },
    global: {
      stubs: {
        "a-tooltip": { template: "<span><slot /></span>" },
        "a-switch": {
          props: ["modelValue"],
          emits: ["change"],
          template: '<button data-testid="share-switch" @click="$emit(\'change\', !modelValue)">{{ modelValue }}</button>'
        },
        "a-modal": { template: '<div role="dialog"><slot /></div>' }
      }
    }
  });
}

beforeEach(() => {
  vi.spyOn(window, "open").mockReturnValue(null);
  modalConfirm.mockReset().mockImplementation(({ onOk }: { onOk: () => void }) => onOk());
  shareApi.getChannelVideoShare.mockReset().mockResolvedValue({ code: 0, data: { enabled: true, pending: true, revision: 3 } });
  shareApi.setChannelVideoShare.mockReset().mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 4 } });
  shareApi.acquireChannelVideoShareLink.mockReset().mockResolvedValue({
    code: 0,
    data: {
      enabled: true,
      pending: false,
      publicId: "share-id",
      signature: "sig-value",
      expiresAt: new Date(Date.now() + 300_000).toISOString(),
      revision: 4
    }
  });
});

describe("ChannelVideoShareControl separated switch and action", () => {
  it("shows the switch without action buttons in the settings row", () => {
    const wrapper = mountControl("switch", { enabled: true, pending: true, revision: 3 });
    expect(wrapper.find('[data-testid="share-switch"]').exists()).toBe(true);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("shows only the share icon in action mode while enabled and reacts to parent state", async () => {
    const wrapper = mountControl("action", { enabled: false, pending: false, revision: 3 });
    expect(wrapper.find('[data-testid="share-switch"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    await wrapper.setProps({ initialState: { enabled: true, pending: true, revision: 4 } });
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(true);
    await wrapper.setProps({ initialState: { enabled: false, pending: false, revision: 5 } });
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    expect(shareApi.getChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("opens a detached page synchronously and never opens a materials dialog", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    let resolveLink!: (value: unknown) => void;
    shareApi.acquireChannelVideoShareLink.mockImplementationOnce(
      () =>
        new Promise(resolve => {
          resolveLink = resolve;
        })
    );
    const wrapper = mountControl("action", { enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    expect(window.open).toHaveBeenCalledWith("", "_blank");
    expect(page.opener).toBeNull();
    expect(page.location.replace).not.toHaveBeenCalled();
    resolveLink({ code: 0, data: { enabled: true, pending: false, revision: 4, publicId: "share-id", signature: "sig-value" } });
    await flushPromises();
    expect(page.location.replace).toHaveBeenCalledWith(expect.stringContaining("share.html#id=share-id&sig=sig-value"));
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("iframe");
    expect(modalConfirm).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("closes the preopened page when acquiring the link fails", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    shareApi.acquireChannelVideoShareLink.mockRejectedValueOnce(new Error("取得失败"));
    const wrapper = mountControl("action", { enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(page.close).toHaveBeenCalledOnce();
    expect(page.location.replace).not.toHaveBeenCalled();
    expect(message.error).toHaveBeenCalledWith("取得失败");
    wrapper.unmount();
  });

  it("keeps a clickable fallback with a clear message when popup is blocked", async () => {
    const wrapper = mountControl("action", { enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('a[aria-label="手动打开分享页"]').attributes("href")).toContain("share.html#id=share-id&sig=sig-value");
    expect(message.warning).toHaveBeenCalledWith(expect.stringContaining("拦截"));
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    await wrapper.setProps({ initialState: { enabled: false, pending: false, revision: 5 } });
    expect(wrapper.find('a[aria-label="手动打开分享页"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("renews an enabled expired link from one click without touching the switch", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    const wrapper = mountControl("action", { enabled: true, pending: false, expired: true, revision: 7 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(window.open).toHaveBeenCalledOnce();
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledExactlyOnceWith(42, 7);
    expect(page.location.replace).toHaveBeenCalledWith(expect.stringContaining("share.html#id=share-id&sig=sig-value"));
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    expect(message.warning).not.toHaveBeenCalled();
    expect(modalConfirm).not.toHaveBeenCalled();
    expect(wrapper.emitted("change")?.at(-1)?.[0]).toEqual(expect.objectContaining({ enabled: true, revision: 4 }));
    wrapper.unmount();
  });

  it("does not acquire or open a page when sharing is disabled", async () => {
    const wrapper = mountControl("action", { enabled: false, pending: false, expired: true, revision: 7 });
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    expect(window.open).not.toHaveBeenCalled();
    expect(shareApi.acquireChannelVideoShareLink).not.toHaveBeenCalled();
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps a newly issued expired-share link clickable when the browser blocks the popup", async () => {
    const wrapper = mountControl("action", { enabled: true, pending: false, expired: true, revision: 7 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledExactlyOnceWith(42, 7);
    expect(wrapper.get('a[aria-label="手动打开分享页"]').attributes("href")).toContain("share.html#id=share-id&sig=sig-value");
    expect(message.warning).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("拦截"));
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("uses server time to refresh once on expiry and does not repeatedly schedule already expired state", async () => {
    vi.useFakeTimers();
    try {
      shareApi.getChannelVideoShare.mockResolvedValue({
        code: 0,
        data: {
          enabled: true,
          pending: false,
          expired: true,
          revision: 4,
          serverTime: "2026-01-01T00:05:00Z",
          expiresAt: "2026-01-01T00:05:00Z"
        }
      });
      const wrapper = mountControl("switch", {
        enabled: true,
        pending: false,
        revision: 3,
        serverTime: "2026-01-01T00:00:00Z",
        expiresAt: "2026-01-01T00:05:00Z"
      });
      await flushPromises();
      await vi.advanceTimersByTimeAsync(299_000);
      expect(shareApi.getChannelVideoShare).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1_100);
      expect(shareApi.getChannelVideoShare).toHaveBeenCalledOnce();
      await vi.advanceTimersByTimeAsync(10_000);
      expect(shareApi.getChannelVideoShare).toHaveBeenCalledOnce();
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("confirms global timing before enabling and supports cancellation", async () => {
    modalConfirm.mockImplementation(({ onCancel }: { onCancel: () => void }) => onCancel());
    const wrapper = mountControl("switch", { enabled: false, pending: false, revision: 3 });
    await wrapper.get('[data-testid="share-switch"]').trigger("click");
    await flushPromises();
    expect(modalConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: "开启视频分享", content: expect.stringContaining("全局配置") })
    );
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("enables only after confirmation and emits updated state for the action control", async () => {
    shareApi.setChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: true, pending: false, revision: 4 } });
    const wrapper = mountControl("switch", { enabled: false, pending: false, revision: 3 });
    await wrapper.get('[data-testid="share-switch"]').trigger("click");
    await flushPromises();
    expect(shareApi.setChannelVideoShare).toHaveBeenCalledExactlyOnceWith(42, true, 3);
    expect(wrapper.emitted("change")?.at(-1)).toEqual([{ enabled: true, pending: false, revision: 4 }]);
    wrapper.unmount();
  });

  it.each(["request", "response"])("stops acquisition after a conflict refresh finds sharing disabled (%s)", async failure => {
    if (failure === "request") shareApi.acquireChannelVideoShareLink.mockRejectedValue({ response: { status: 409 } });
    else shareApi.acquireChannelVideoShareLink.mockResolvedValue({ code: 409, message: "分享已更新" });
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 5 } });
    const wrapper = mountControl("action", { enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(shareApi.getChannelVideoShare).toHaveBeenCalledTimes(1);
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledExactlyOnceWith(42, 3);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    wrapper.unmount();
  });
  it.each(["request", "response"])(
    "refreshes a conflicted revision and retries once in the original popup (%s)",
    async failure => {
      const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
      vi.mocked(window.open).mockReturnValue(page as unknown as Window);
      if (failure === "request") shareApi.acquireChannelVideoShareLink.mockRejectedValueOnce({ response: { status: 409 } });
      else shareApi.acquireChannelVideoShareLink.mockResolvedValueOnce({ code: 409, message: "分享已更新" });
      shareApi.getChannelVideoShare.mockResolvedValue({
        code: 0,
        data: { enabled: true, expired: true, pending: false, revision: 8 }
      });
      const wrapper = mountControl("action", { enabled: true, pending: false, expired: true, revision: 7 });
      await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
      await flushPromises();
      expect(shareApi.getChannelVideoShare).toHaveBeenCalledOnce();
      expect(shareApi.acquireChannelVideoShareLink.mock.calls).toEqual([
        [42, 7],
        [42, 8]
      ]);
      expect(window.open).toHaveBeenCalledOnce();
      expect(page.location.replace).toHaveBeenCalledWith(expect.stringContaining("sig=sig-value"));
      expect(page.close).not.toHaveBeenCalled();
      expect(message.error).not.toHaveBeenCalled();
      expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
      wrapper.unmount();
    }
  );

  it("does not retry indefinitely when the refreshed revision also conflicts", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    shareApi.acquireChannelVideoShareLink.mockResolvedValue({ code: 409, message: "分享已更新" });
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: true, pending: false, revision: 8 } });
    const wrapper = mountControl("action", { enabled: true, pending: false, revision: 7 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledTimes(2);
    expect(shareApi.getChannelVideoShare).toHaveBeenCalledOnce();
    expect(page.close).toHaveBeenCalledOnce();
    expect(page.location.replace).not.toHaveBeenCalled();
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("does not retry a revoked link and refreshes the disabled state without asking to toggle", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    shareApi.acquireChannelVideoShareLink.mockRejectedValueOnce({ response: { status: 410 } });
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 8 } });
    const wrapper = mountControl("action", { enabled: true, pending: false, revision: 7 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledExactlyOnceWith(42, 7);
    expect(shareApi.getChannelVideoShare).toHaveBeenCalledOnce();
    expect(message.warning).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("分享已关闭或访问权限已变更"));
    expect(page.close).toHaveBeenCalledOnce();
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
