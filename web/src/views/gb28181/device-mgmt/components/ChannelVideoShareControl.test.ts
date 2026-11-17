import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChannelVideoShareControl from "./ChannelVideoShareControl.vue";

const shareApi = vi.hoisted(() => ({
  acquireChannelVideoShareLink: vi.fn(),
  getChannelVideoShare: vi.fn(),
  setChannelVideoShare: vi.fn(),
  regenerateChannelVideoShare: vi.fn()
}));
const modalConfirm = vi.hoisted(() => vi.fn());

vi.mock("@/api/gb28181", () => shareApi);
vi.mock("@arco-design/web-vue", async importOriginal => {
  const actual = await importOriginal<typeof import("@arco-design/web-vue")>();
  return { ...actual, Modal: { ...actual.Modal, confirm: modalConfirm } };
});
vi.mock("@/components/s-qrcode-draw/index.vue", () => ({
  default: { props: ["text"], template: '<img data-testid="share-qrcode" :alt="text" />' }
}));

function mountControl(initialState?: {
  enabled: boolean;
  pending: boolean;
  revision: number;
  expiresAt?: string;
  serverTime?: string;
  expired?: boolean;
}) {
  return mount(ChannelVideoShareControl, {
    props: { channelId: 42, channelName: "门岗摄像头", initialState },
    global: {
      stubs: {
        "a-tooltip": { template: "<span><slot /></span>" },
        "a-switch": {
          props: ["modelValue"],
          emits: ["change"],
          template: '<button data-testid="share-switch" @click="$emit(\'change\', !modelValue)">{{ modelValue }}</button>'
        },
        "a-modal": { template: '<div v-if="visible"><slot /></div>', props: ["visible"] },
        "a-button": { emits: ["click"], template: '<button @click="$emit(\'click\')"><slot /><slot name="icon" /></button>' }
      }
    }
  });
}

describe("ChannelVideoShareControl", () => {
  let renewedExpiry: string;
  beforeEach(() => {
    vi.spyOn(window, "open").mockReturnValue(null);
    renewedExpiry = new Date(Date.now() + 600_000).toISOString();
    modalConfirm.mockReset().mockImplementation(({ onOk }: { onOk: () => void }) => onOk());
    shareApi.getChannelVideoShare.mockReset().mockResolvedValue({
      code: 0,
      data: { enabled: true, pending: true, revision: 3 }
    });
    shareApi.setChannelVideoShare.mockReset().mockResolvedValue({
      code: 0,
      data: { enabled: false, pending: false, revision: 4 }
    });
    shareApi.regenerateChannelVideoShare.mockReset().mockResolvedValue({
      code: 0,
      data: {
        enabled: true,
        pending: false,
        publicId: "share-id",
        signature: "new-signature",
        expiresAt: renewedExpiry,
        revision: 5
      }
    });
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

  it("opens a detached page synchronously before link acquisition completes", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    let resolveLink!: (value: unknown) => void;
    shareApi.acquireChannelVideoShareLink.mockImplementationOnce(
      () =>
        new Promise(resolve => {
          resolveLink = resolve;
        })
    );
    const wrapper = mountControl({ enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    expect(window.open).toHaveBeenCalledWith("", "_blank");
    expect(page.opener).toBeNull();
    expect(page.location.replace).not.toHaveBeenCalled();
    resolveLink({ code: 0, data: { enabled: true, pending: false, revision: 4, publicId: "share-id", signature: "sig-value" } });
    await flushPromises();
    expect(page.location.replace).toHaveBeenCalledWith(expect.stringContaining("share.html#id=share-id&sig=sig-value"));
    wrapper.unmount();
  });

  it("closes the preopened page when acquiring the link fails", async () => {
    const page = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(page as unknown as Window);
    shareApi.acquireChannelVideoShareLink.mockRejectedValueOnce(new Error("取得失败"));
    const wrapper = mountControl({ enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(page.close).toHaveBeenCalledOnce();
    expect(page.location.replace).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps a clickable link when the browser blocks the new page", async () => {
    const wrapper = mountControl({ enabled: true, pending: true, revision: 3 });
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('a[aria-label="手动打开分享页"]').attributes("href")).toContain("share.html#id=share-id&sig=sig-value");
    wrapper.unmount();
  });

  it("shows pending, expiry and expired lifecycle state in the channel row", async () => {
    const wrapper = mountControl({ enabled: true, pending: true, revision: 3 });
    expect(wrapper.get('[data-testid="share-lifecycle"]').text()).toContain("待取得链接");
    await wrapper.setProps({ initialState: { enabled: true, pending: false, revision: 4, expiresAt: renewedExpiry } });
    expect(wrapper.get('[data-testid="share-lifecycle"]').text()).toContain("有效期至");
    await wrapper.setProps({ initialState: { enabled: false, pending: false, revision: 5, expired: true } });
    expect(wrapper.get('[data-testid="share-lifecycle"]').text()).toBe("已过期");
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    await wrapper.setProps({ initialState: { enabled: false, pending: false, revision: 6 } });
    expect(wrapper.find('[data-testid="share-lifecycle"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("uses the batch list state without a request for each row and accepts refreshed parent state", async () => {
    const wrapper = mountControl({ enabled: true, pending: true, revision: 8 });
    await flushPromises();
    expect(shareApi.getChannelVideoShare).not.toHaveBeenCalled();
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(true);
    await wrapper.setProps({ initialState: { enabled: false, pending: false, revision: 9 } });
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("uses server time for expiry and clears old materials when refreshed state changes", async () => {
    vi.useFakeTimers();
    try {
      const wrapper = mountControl({
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
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("loads pending state and acquires one-time share material from the share icon", async () => {
    const wrapper = mountControl();
    await flushPromises();

    expect(shareApi.getChannelVideoShare).toHaveBeenCalledWith(42);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(true);
    await wrapper.find('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();

    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledWith(42, 3);
    expect(wrapper.text()).toContain("iframe 嵌入代码");
    expect(wrapper.text()).toContain("share.html#id=share-id&sig=sig-value");
    expect(wrapper.find('[data-testid="share-qrcode"]').exists()).toBe(true);
    const embed = wrapper.find("pre").text();
    const template = document.createElement("template");
    template.innerHTML = embed;
    const src = template.content.querySelector("iframe")?.getAttribute("src");
    expect(src).toBeTruthy();
    const fragment = new URLSearchParams(new URL(src!).hash.slice(1));
    expect(fragment.get("embed")).toBe("1");
    expect(fragment.get("id")).toBe("share-id");
    expect(fragment.get("sig")).toBe("sig-value");
    wrapper.unmount();
  });

  it("sends the latest revision when the operator disables sharing", async () => {
    const wrapper = mountControl();
    await flushPromises();
    await wrapper.find('[data-testid="share-switch"]').trigger("click");
    await flushPromises();

    expect(shareApi.setChannelVideoShare).toHaveBeenCalledWith(42, false, 3);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    wrapper.unmount();
  });

  async function openMaterials() {
    const wrapper = mountControl();
    await flushPromises();
    await wrapper.get('[aria-label="打开通道分享页"]').trigger("click");
    await flushPromises();
    return wrapper;
  }

  it("confirms global expiry and immediate timing before enabling, and supports cancellation", async () => {
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 3 } });
    modalConfirm.mockImplementation(({ onCancel }: { onCancel: () => void }) => onCancel());
    const wrapper = mountControl();
    await flushPromises();
    await wrapper.get('[data-testid="share-switch"]').trigger("click");
    await flushPromises();
    expect(modalConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: "开启视频分享", content: expect.stringContaining("全局配置") })
    );
    expect(modalConfirm.mock.calls[0][0].content).toContain("立即开始计时");
    expect(shareApi.setChannelVideoShare).not.toHaveBeenCalled();
    expect(wrapper.find("input").exists()).toBe(false);
    wrapper.unmount();
  });

  it("enables with the latest revision after confirmation without a channel expiry input", async () => {
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 3 } });
    shareApi.setChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: true, pending: false, revision: 4 } });
    const wrapper = mountControl();
    await flushPromises();
    await wrapper.get('[data-testid="share-switch"]').trigger("click");
    await flushPromises();
    expect(shareApi.setChannelVideoShare).toHaveBeenCalledExactlyOnceWith(42, true, 3);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(true);
    expect(wrapper.find("input").exists()).toBe(false);
    wrapper.unmount();
  });

  it("does not regenerate when permanent invalidation confirmation is cancelled", async () => {
    const wrapper = await openMaterials();
    modalConfirm.mockImplementation(({ onCancel }: { onCancel: () => void }) => onCancel());
    await wrapper.get('[aria-label="重新生成分享链接"]').trigger("click");
    await flushPromises();
    expect(modalConfirm).toHaveBeenCalledWith(expect.objectContaining({ content: expect.stringContaining("永久失效") }));
    expect(shareApi.regenerateChannelVideoShare).not.toHaveBeenCalled();
    expect(wrapper.get("code").text()).toContain("sig-value");
    wrapper.unmount();
  });

  it("replaces link, iframe, QR and expiry only after regeneration succeeds", async () => {
    const wrapper = await openMaterials();
    await wrapper.get('[aria-label="重新生成分享链接"]').trigger("click");
    await flushPromises();
    expect(shareApi.regenerateChannelVideoShare).toHaveBeenCalledExactlyOnceWith(42, 4);
    expect(wrapper.get("code").text()).toContain("new-signature");
    expect(wrapper.get("pre").text()).toContain("new-signature");
    expect(wrapper.get('[data-testid="share-qrcode"]').attributes("alt")).toContain("new-signature");
    expect(wrapper.text()).toContain(new Date(renewedExpiry).toLocaleString());
    expect(wrapper.text()).not.toContain("sig-value");
    expect(wrapper.text()).toContain("打开分享页");
    wrapper.unmount();
  });

  it.each(["request", "response"])("preserves existing materials when regeneration fails (%s)", async failure => {
    const wrapper = await openMaterials();
    const material = wrapper.get("code").text();
    const embed = wrapper.get("pre").text();
    const expiry = wrapper.get(".share-dialog__heading span").text();
    if (failure === "request") shareApi.regenerateChannelVideoShare.mockRejectedValue(new Error("网络中断"));
    else shareApi.regenerateChannelVideoShare.mockResolvedValue({ code: 500, message: "保存失败" });
    await wrapper.get('[aria-label="重新生成分享链接"]').trigger("click");
    await flushPromises();
    expect(wrapper.get("code").text()).toBe(material);
    expect(wrapper.get("pre").text()).toBe(embed);
    expect(wrapper.get(".share-dialog__heading span").text()).toBe(expiry);
    expect(shareApi.getChannelVideoShare).toHaveBeenCalledTimes(1);
    expect(shareApi.regenerateChannelVideoShare).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it.each(["request", "response"])("refreshes state after conflict without retrying (%s)", async failure => {
    const wrapper = await openMaterials();
    shareApi.getChannelVideoShare.mockResolvedValue({ code: 0, data: { enabled: false, pending: false, revision: 6 } });
    if (failure === "request") shareApi.regenerateChannelVideoShare.mockRejectedValue({ response: { status: 409 } });
    else shareApi.regenerateChannelVideoShare.mockResolvedValue({ code: 409, message: "分享已更新" });
    await wrapper.get('[aria-label="重新生成分享链接"]').trigger("click");
    await flushPromises();
    expect(shareApi.getChannelVideoShare).toHaveBeenCalledTimes(2);
    expect(shareApi.regenerateChannelVideoShare).toHaveBeenCalledTimes(1);
    expect(shareApi.acquireChannelVideoShareLink).toHaveBeenCalledTimes(1);
    expect(wrapper.find('[aria-label="打开通道分享页"]').exists()).toBe(false);
    expect(wrapper.find("code").exists()).toBe(false);
    wrapper.unmount();
  });
});
