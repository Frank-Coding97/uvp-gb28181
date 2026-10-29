import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, nextTick } from "vue";

const api = vi.hoisted(() => ({
  getNotice: vi.fn(),
  ignore: vi.fn(),
  clearIgnore: vi.fn(),
  setAutoCheck: vi.fn()
}));

vi.mock("@/api/service-upgrade", () => ({
  getServiceUpgradeNotice: api.getNotice,
  ignoreServiceUpgradeNotice: api.ignore,
  clearIgnoredServiceUpgradeNotice: api.clearIgnore,
  setServiceUpgradeAutoCheck: api.setAutoCheck
}));

const permissions = vi.hoisted(() => ({ value: [] as string[] }));
vi.mock("@/hooks/usePermission", () => ({
  hasPermission: (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission)
}));

import { useUpgradeNotice } from "./useUpgradeNotice";

const Host = defineComponent({
  setup() {
    return useUpgradeNotice();
  },
  template: "<div />"
});

const notice = (overrides: Record<string, unknown> = {}) => ({
  currentVersion: "1.1.0",
  latestVersion: "1.2.0",
  hasUpdate: true,
  ignored: false,
  checkEnabled: true,
  autoCheck: true,
  checkedAt: "2026-10-29T02:00:00Z",
  ...overrides
});

const mountHost = async () => {
  const wrapper = mount(Host);
  await nextTick();
  await Promise.resolve();
  return wrapper;
};

describe("useUpgradeNotice", () => {
  beforeEach(() => {
    permissions.value = ["system:upgrade:read", "system:upgrade:manage"];
    api.getNotice.mockReset().mockResolvedValue({ data: notice() });
    api.ignore.mockReset().mockResolvedValue({ data: {} });
    api.clearIgnore.mockReset().mockResolvedValue({ data: {} });
    api.setAutoCheck.mockReset().mockResolvedValue({ data: { enabled: false } });
  });

  afterEach(() => vi.clearAllMocks());

  it("有新版本且未忽略时点亮橙点", async () => {
    const wrapper = await mountHost();
    expect(api.getNotice).toHaveBeenCalledTimes(1);
    expect((wrapper.vm as any).pending).toBe(true);
    expect((wrapper.vm as any).latestVersion).toBe("1.2.0");
    wrapper.unmount();
  });

  it("忽略过的版本不再点亮", async () => {
    api.getNotice.mockResolvedValue({ data: notice({ ignored: true }) });
    const wrapper = await mountHost();
    expect((wrapper.vm as any).pending).toBe(false);
    expect((wrapper.vm as any).notice.ignored).toBe(true);
    wrapper.unmount();
  });

  // 离线部署没有官方源，不能因为查不到更新就长期亮着一个点。
  it("未配置官方源时不点亮", async () => {
    api.getNotice.mockResolvedValue({ data: notice({ checkEnabled: false }) });
    const wrapper = await mountHost();
    expect((wrapper.vm as any).pending).toBe(false);
    wrapper.unmount();
  });

  it("没有查看权限时既不请求也不显示", async () => {
    permissions.value = [];
    const wrapper = await mountHost();
    expect(api.getNotice).not.toHaveBeenCalled();
    expect((wrapper.vm as any).pending).toBe(false);
    wrapper.unmount();
  });

  // 接口失败（助手没起来 / 断网）必须静默回退，不能把异常抛到布局层。
  it("读取失败时静默回退为仅显示本地版本号", async () => {
    api.getNotice.mockRejectedValue(new Error("503"));
    const wrapper = await mountHost();
    expect((wrapper.vm as any).pending).toBe(false);
    expect((wrapper.vm as any).currentVersion).toBeTruthy();
    wrapper.unmount();
  });

  it("忽略当前版本后重新读取状态", async () => {
    const wrapper = await mountHost();
    api.getNotice.mockResolvedValue({ data: notice({ ignored: true }) });
    await (wrapper.vm as any).ignoreCurrentVersion();
    expect(api.ignore).toHaveBeenCalledWith("1.2.0");
    expect(api.getNotice).toHaveBeenCalledTimes(2);
    expect((wrapper.vm as any).pending).toBe(false);
    wrapper.unmount();
  });

  it("取消忽略后重新点亮", async () => {
    api.getNotice.mockResolvedValue({ data: notice({ ignored: true }) });
    const wrapper = await mountHost();
    api.getNotice.mockResolvedValue({ data: notice({ ignored: false }) });
    await (wrapper.vm as any).restoreIgnoredVersion();
    expect(api.clearIgnore).toHaveBeenCalledTimes(1);
    expect((wrapper.vm as any).pending).toBe(true);
    wrapper.unmount();
  });

  // 后端缺字段代表「从没关过」，必须当成开启，否则老版本后端一上线开关就变灰。
  it("后端未返回开关字段时按开启处理", async () => {
    api.getNotice.mockResolvedValue({ data: notice({ autoCheck: undefined }) });
    const wrapper = await mountHost();
    expect((wrapper.vm as any).autoCheckEnabled).toBe(true);
    wrapper.unmount();
  });

  it("关闭定时检查后写回并重新读取状态", async () => {
    const wrapper = await mountHost();
    api.getNotice.mockResolvedValue({ data: notice({ autoCheck: false }) });
    const saved = await (wrapper.vm as any).setAutoCheck(false);
    expect(saved).toBe(true);
    expect(api.setAutoCheck).toHaveBeenCalledWith(false);
    expect((wrapper.vm as any).autoCheckEnabled).toBe(false);
    wrapper.unmount();
  });

  it("保存失败时返回 false 且不改变本地状态", async () => {
    const wrapper = await mountHost();
    api.setAutoCheck.mockRejectedValue(new Error("500"));
    const saved = await (wrapper.vm as any).setAutoCheck(false);
    expect(saved).toBe(false);
    expect((wrapper.vm as any).autoCheckEnabled).toBe(true);
    wrapper.unmount();
  });

  // 离线部署/助手未运行时开关点了也不会有任何效果，必须置灰。
  it("没有官方源时开关不可设置", async () => {
    api.getNotice.mockResolvedValue({ data: notice({ checkEnabled: false }) });
    const wrapper = await mountHost();
    expect((wrapper.vm as any).canSetAutoCheck).toBe(false);
    wrapper.unmount();
  });

  it("只有查看权限时开关不可设置", async () => {
    permissions.value = ["system:upgrade:read"];
    const wrapper = await mountHost();
    expect((wrapper.vm as any).canSetAutoCheck).toBe(false);
    wrapper.unmount();
  });
});
