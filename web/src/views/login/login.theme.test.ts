import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import persistedstate from "pinia-plugin-persistedstate";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick, ref, watch } from "vue";
import LoginPage from "./login.vue";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useThemeMethods } from "@/hooks/useThemeMethods";

const testStorage = vi.hoisted(() => {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key)
  };
});

vi.mock("@/store/config/index", () => ({ default: (key: string) => ({ key, storage: testStorage }) }));
vi.mock("./components/login-form.vue", () => ({ default: { template: "<div />" } }));
vi.mock("@/store/modules/sys-config", async () => {
  const { defineStore } = await import("pinia");
  return {
    useSysConfigStore: defineStore("login-test-config", () => ({
      systemConfig: ref({ systemCopyright: "", systemRecordNo: "" })
    }))
  };
});

const createPage = () => {
  const pinia = createPinia().use(persistedstate);
  const wrapper = mount(LoginPage, { global: { plugins: [pinia] } });
  useThemeMethods().initTheme();
  return { wrapper, theme: useThemeConfig(pinia) };
};

beforeEach(() => {
  vi.stubGlobal("watch", watch);
  testStorage.removeItem("theme-config");
  document.body.removeAttribute("arco-theme");
});

afterEach(() => {
  testStorage.removeItem("theme-config");
  document.body.removeAttribute("arco-theme");
  vi.unstubAllGlobals();
});

describe("login theme", () => {
  it("places the architecture between the introduction and feature summary in the brand panel", () => {
    const { wrapper } = createPage();
    const panel = wrapper.get(".brand-side");
    expect(panel.find('svg.architecture[role="img"]').exists()).toBe(true);
    const children = Array.from(panel.element.children);
    expect(children.indexOf(panel.get(".brand-lede").element)).toBeLessThan(children.indexOf(panel.get(".architecture").element));
    expect(children.indexOf(panel.get(".architecture").element)).toBeLessThan(
      children.indexOf(panel.get(".brand-features").element)
    );
    wrapper.unmount();
  });

  it("places the complete brand in the page header outside the login card", () => {
    const { wrapper } = createPage();
    expect(wrapper.find(".page-meta .brand-heading .brand-mark").exists()).toBe(true);
    expect(wrapper.get(".page-meta .brand-title").text()).toContain("统一视频接入平台");
    expect(wrapper.get(".page-meta .brand-title").text()).toContain("GB28181");
    expect(wrapper.get(".float-card").find(".brand-heading").exists()).toBe(false);
    expect(wrapper.get(".float-card").find(".brand-title").exists()).toBe(false);
    wrapper.unmount();
  });

  it("places the theme toggle inside the login card and keeps the page version", () => {
    const { wrapper } = createPage();
    expect(wrapper.get(".float-card").find("#login-theme-toggle").exists()).toBe(true);
    expect(wrapper.get(".page-meta").find("#login-theme-toggle").exists()).toBe(false);
    expect(wrapper.get(".page-meta .version-text").text()).toContain("GB/T 28181-2022");
    wrapper.unmount();
  });

  it("switches the login page and shared application theme in both directions", async () => {
    const { wrapper, theme } = createPage();
    const button = wrapper.get("#login-theme-toggle");
    expect(button.attributes("aria-label")).toBe("切换到暗色模式");
    await button.trigger("click");
    expect(theme.darkMode).toBe(true);
    expect(document.body.getAttribute("arco-theme")).toBe("dark");
    expect(wrapper.get(".login-page").classes()).toContain("dark-mode");
    expect(button.attributes("aria-label")).toBe("切换到明亮模式");
    await button.trigger("click");
    expect(theme.darkMode).toBe(false);
    expect(document.body.hasAttribute("arco-theme")).toBe(false);
    expect(wrapper.get(".login-page").classes()).not.toContain("dark-mode");
    wrapper.unmount();
  });

  it("restores the chosen mode when the application is recreated", async () => {
    const first = createPage();
    await first.wrapper.get("#login-theme-toggle").trigger("click");
    await nextTick();
    first.wrapper.unmount();
    const restored = createPage();
    expect(restored.theme.darkMode).toBe(true);
    expect(restored.wrapper.get(".login-page").classes()).toContain("dark-mode");
    expect(document.body.getAttribute("arco-theme")).toBe("dark");
    expect(restored.wrapper.get("#login-theme-toggle").attributes("aria-pressed")).toBe("true");
    restored.wrapper.unmount();
  });
});
