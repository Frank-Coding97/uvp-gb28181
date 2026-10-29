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
// pending 是唯一开关：没有它侧边栏什么都不画。
const upgrade = vi.hoisted(() => ({
  state: {
    pending: false,
    currentVersion: "1.1.15",
    latestVersion: "1.1.16",
    checkedAt: "2026-10-29T02:00:00Z",
    canIgnore: true
  }
}));

vi.mock("@/hooks/useUpgradeNotice", () => ({
  useUpgradeNotice: () => ({
    canIgnore: computed(() => upgrade.state.canIgnore),
    currentVersion: computed(() => upgrade.state.currentVersion),
    latestVersion: computed(() => upgrade.state.latestVersion),
    checkedAt: computed(() => upgrade.state.checkedAt),
    pending: computed(() => upgrade.state.pending),
    refreshing: ref(false),
    ignoring: ref(false),
    refresh: vi.fn(),
    ignoreCurrentVersion: vi.fn()
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
    upgrade.state.pending = false;
    upgrade.state.canIgnore = true;
  });

  it("fills the 40px logo frame without an inset", () => {
    const wrapper = mountLogo();

    expect(wrapper.find(".logo-svg").attributes("width")).toBe("40");
    expect(wrapper.find(".logo-svg").attributes("height")).toBe("40");
    expect(wrapper.find(".logo_mark").attributes("style")).toBeUndefined();
  });

  // ⛔ 侧边栏不常显版本号：当前版本在服务升级页首屏，这里只在有更新时提示。
  it("没有新版本时完全不渲染版本徽标", async () => {
    const wrapper = mountLogo();
    await nextTick();

    expect(wrapper.find('[data-testid="logo-version"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="logo-version-card"]').exists()).toBe(false);
    // 连纯文字兜底也不留：产品名右边保持留白。
    expect(wrapper.text()).not.toContain("v1.1.15");
    wrapper.unmount();
  });

  it("有新版本时冒橙色胶囊，点开给出最新版本与升级入口", async () => {
    upgrade.state.pending = true;
    const wrapper = mountLogo();
    await nextTick();

    const badge = wrapper.get('[data-testid="logo-version"]');
    // 徽标显示的是「待装的新版本」，不是当前版本。
    expect(badge.text()).toContain("v1.1.16");
    expect(badge.text()).not.toContain("v1.1.15");
    expect(badge.attributes("aria-label")).toContain("当前版本 v1.1.15");
    expect(wrapper.find(".logo_version_dot").exists()).toBe(true);

    const card = wrapper.get('[data-testid="logo-version-card"]');
    expect(card.text()).toContain("v1.1.15");
    expect(card.text()).toContain("v1.1.16");
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

  // 被忽略、离线部署、没有查看权限：三种情况都由 pending 兜住，一律不显示。
  it("被忽略或被隐藏时徽标同样不出现", async () => {
    upgrade.state.pending = false;
    const wrapper = mountLogo();
    await nextTick();

    expect(wrapper.find('[data-testid="logo-version"]').exists()).toBe(false);
    expect(wrapper.find(".logo_version_dot").exists()).toBe(false);
    wrapper.unmount();
  });

  // 没有忽略权限时，卡片不给「忽略此版本」，避免点下去 403。
  it("没有管理权限时卡片不提供忽略入口", async () => {
    upgrade.state.pending = true;
    upgrade.state.canIgnore = false;
    const wrapper = mountLogo();
    await nextTick();

    const card = wrapper.get('[data-testid="logo-version-card"]');
    expect(card.text()).not.toContain("忽略此版本");
    wrapper.unmount();
  });
});
