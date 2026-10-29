import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { computed, nextTick, ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("computed", computed);

vi.mock("@/store/modules/theme-config", async () => {
  const { defineStore } = await import("pinia");
  const { ref } = await import("vue");
  return {
    useThemeConfig: defineStore("theme-config", () => ({
      collapsed: ref(false),
      asideDark: ref(false),
      layoutType: ref("layoutDefaults")
    }))
  };
});

vi.mock("@/store/modules/sys-config", async () => {
  const { defineStore } = await import("pinia");
  const { ref } = await import("vue");
  return {
    useSysConfigStore: defineStore("sys-config", () => ({
      systemConfig: ref({ systemName: "UVP 统一视频接入平台", systemLogo: "" })
    }))
  };
});

vi.mock("@/utils/app", () => ({
  handleUrl: (url?: string) => url || ""
}));

vi.mock("@/components/s-logo/index.vue", () => ({
  default: {
    name: "LogoSvg",
    props: ["width", "height"],
    template: "<img class='logo-svg' :width='width' :height='height' />"
  }
}));

const routerPush = vi.hoisted(() => vi.fn());
vi.mock("vue-router", () => ({
  useRouter: () => ({ push: routerPush })
}));

// 版本徽标的数据来自后台检查缓存；这里只验证「拿到什么就画什么」，
// 取数与忽略语义由 useUpgradeNotice 自己的用例覆盖。
const upgrade = vi.hoisted(() => ({
  state: {
    notice: undefined as any,
    canRead: true,
    canIgnore: true,
    pending: false
  }
}));

vi.mock("@/hooks/useUpgradeNotice", () => ({
  useUpgradeNotice: () => ({
    notice: computed(() => upgrade.state.notice),
    canRead: computed(() => upgrade.state.canRead),
    canIgnore: computed(() => upgrade.state.canIgnore),
    currentVersion: computed(() => upgrade.state.notice?.currentVersion || "1.1.0"),
    latestVersion: computed(() => upgrade.state.notice?.latestVersion || ""),
    checkedAt: computed(() => upgrade.state.notice?.checkedAt || ""),
    pending: computed(() => upgrade.state.pending),
    refreshing: ref(false),
    ignoring: ref(false),
    refresh: vi.fn(),
    ignoreCurrentVersion: vi.fn(),
    restoreIgnoredVersion: vi.fn()
  })
}));

import Logo from "./index.vue";

const PopoverStub = {
  name: "APopover",
  props: ["popupVisible", "trigger", "position"],
  emits: ["update:popupVisible"],
  template: `<div class="popover-stub"><slot /><div class="popover-content"><slot name="content" /></div></div>`
};

const mountLogo = () =>
  mount(Logo, {
    global: {
      // icon-refresh 由 Arco 自动注册，测试环境没有全局注册，桩掉即可。
      stubs: { "a-popover": PopoverStub, "icon-refresh": true }
    }
  });

describe("Logo", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    upgrade.state = { notice: undefined, canRead: true, canIgnore: true, pending: false };
  });

  it("fills the 40px logo frame without an inset", () => {
    const wrapper = mountLogo();

    expect(wrapper.find(".logo-svg").attributes("width")).toBe("40");
    expect(wrapper.find(".logo-svg").attributes("height")).toBe("40");
    expect(wrapper.find(".logo_mark").attributes("style")).toBeUndefined();
  });

  it("常显当前版本号，无更新时不冒橙点", async () => {
    upgrade.state.notice = {
      currentVersion: "1.1.15",
      latestVersion: "",
      hasUpdate: false,
      ignored: false,
      checkEnabled: true
    };
    const wrapper = mountLogo();
    await nextTick();

    const badge = wrapper.get('[data-testid="logo-version"]');
    expect(badge.text()).toContain("v1.1.15");
    expect(badge.classes()).not.toContain("logo_version--pending");
    expect(wrapper.find(".logo_version_dot").exists()).toBe(false);
    wrapper.unmount();
  });

  it("有新版本时变橙并冒点，卡片里给出最新版本与升级入口", async () => {
    upgrade.state.notice = {
      currentVersion: "1.1.15",
      latestVersion: "1.1.16",
      hasUpdate: true,
      ignored: false,
      checkEnabled: true
    };
    upgrade.state.pending = true;
    const wrapper = mountLogo();
    await nextTick();

    const badge = wrapper.get('[data-testid="logo-version"]');
    expect(badge.classes()).toContain("logo_version--pending");
    expect(wrapper.find(".logo_version_dot").exists()).toBe(true);

    const card = wrapper.get('[data-testid="logo-version-card"]');
    expect(card.text()).toContain("v1.1.15");
    expect(card.text()).toContain("最新版本：v1.1.16");
    expect(card.text()).toContain("有新版本可用！");
    expect(card.text()).toContain("前往升级");

    await card
      .findAll("button")
      .find(node => node.text() === "前往升级")!
      .trigger("click");
    // 带 check=1 让升级页落地即自动检查一次。
    expect(routerPush).toHaveBeenCalledWith({ path: "/system/service-upgrade", query: { check: "1" } });
    wrapper.unmount();
  });

  // 离线部署没有官方源：徽标只显示版本号，卡片说明为什么没有更新信息。
  it("未配置官方源时只给出版本号与说明", async () => {
    upgrade.state.notice = {
      currentVersion: "1.1.15",
      latestVersion: "",
      hasUpdate: false,
      ignored: false,
      checkEnabled: false
    };
    const wrapper = mountLogo();
    await nextTick();

    const card = wrapper.get('[data-testid="logo-version-card"]');
    expect(card.text()).toContain("未配置官方更新源");
    expect(card.text()).not.toContain("前往升级");
    wrapper.unmount();
  });

  it("被忽略时说明该版本已忽略并给出取消入口", async () => {
    upgrade.state.notice = {
      currentVersion: "1.1.15",
      latestVersion: "1.1.16",
      hasUpdate: true,
      ignored: true,
      checkEnabled: true
    };
    const wrapper = mountLogo();
    await nextTick();

    const card = wrapper.get('[data-testid="logo-version-card"]');
    expect(card.text()).toContain("已忽略 v1.1.16");
    expect(card.text()).toContain("取消忽略");
    expect(wrapper.find(".logo_version_dot").exists()).toBe(false);
    wrapper.unmount();
  });

  // 没有「查看服务升级」权限的账号直接显示纯文字，避免点开就 403。
  it("没有查看权限时降级为不可点击的版本文字", async () => {
    upgrade.state.canRead = false;
    upgrade.state.notice = undefined;
    const wrapper = mountLogo();
    await nextTick();

    expect(wrapper.find('[data-testid="logo-version"]').exists()).toBe(false);
    expect(wrapper.find(".logo_version--plain").text()).toContain("v");
    wrapper.unmount();
  });
});
