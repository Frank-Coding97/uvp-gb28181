import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/store/modules/theme-config", async () => {
  const { defineStore } = await import("pinia");
  const { ref } = await import("vue");
  return {
    useThemeConfig: defineStore("theme-config", () => {
      const collapsed = ref(false);
      const asideDark = ref(false);
      function setCollapsed(value: boolean) {
        collapsed.value = value;
      }
      return { collapsed, asideDark, setCollapsed };
    })
  };
});

vi.mock("@/store/modules/route-config", async () => {
  const { defineStore } = await import("pinia");
  const { ref } = await import("vue");
  return {
    useRouteConfigStore: defineStore("route-config", () => {
      const routeTree = ref([]);
      return { routeTree };
    })
  };
});

import Aside from "./index.vue";
import { useThemeConfig } from "@/store/modules/theme-config";

const stubs = {
  Logo: { template: "<div class='logo' />" },
  Menu: { props: ["routeTree"], template: "<div class='menu' />" },
  "a-layout-sider": { props: ["collapsed", "width"], template: "<div class='sider'><slot /></div>" },
  "a-scrollbar": { template: "<div><slot /></div>" }
};

describe("Aside", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("marks the sidebar collapsed so the shell can release its width", async () => {
    const theme = useThemeConfig();
    const wrapper = mount(Aside, { global: { stubs } });

    expect(wrapper.classes()).toContain("aside");
    expect(wrapper.classes()).not.toContain("collapsed");

    theme.setCollapsed(true);
    await nextTick();

    expect(wrapper.classes()).toContain("collapsed");
  });
});
