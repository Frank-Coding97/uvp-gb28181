import { flushPromises, mount } from "@vue/test-utils";
import { reactive, ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Tabs from "./index.vue";

let routeStore: any;
let themeStore: any;
const push = vi.fn();
vi.mock("@/store/modules/route-config", () => ({ useRouteConfigStore: () => routeStore }));
vi.mock("@/store/modules/theme-config", () => ({ useThemeConfig: () => themeStore }));
vi.mock("@/hooks/useThemeMethods", () => ({ useThemeMethods: () => ({ setDarkMode: vi.fn() }) }));
vi.mock("vue-router", () => ({ useRouter: () => ({ push }) }));
vi.mock("@/layout/components/Menu/menu-item-icon.vue", () => ({ default: { template: "<i />" } }));

function renderTabs() {
  return mount(Tabs, { global: {
    mocks: { $t: (key: string) => key },
    stubs: {
      "a-tabs": { template: "<div><slot /></div>" },
      "a-tab-pane": { template: "<div><slot name='title' /></div>" },
      "a-dropdown": { template: "<div class='dropdown'><slot /><slot name='content' /></div>" },
      "a-doption": { props: ["disabled"], template: "<button :disabled='disabled'><slot /></button>" },
      "a-tooltip": { template: "<span><slot /></span>" },
      "a-space": { template: "<div><slot /></div>" },
      "icon-refresh": true, "icon-close": true, "icon-left": true, "icon-right": true,
      "icon-close-circle": true, "icon-folder-delete": true, "icon-fullscreen": true,
      "icon-fullscreen-exit": true, "icon-sun-fill": true, "icon-moon-fill": true
    }
  } });
}

beforeEach(() => {
  const tabs = ["/home", "/a", "/b", "/c"].map(path => ({ path, meta: { title: path, affix: path === "/home", keepAlive: true } }));
  routeStore = reactive({
    tabsList: ref(tabs), currentRoute: ref(tabs[3]),
    removeTabsList: vi.fn((path: string) => { routeStore.tabsList = routeStore.tabsList.filter((t: any) => t.path !== path); }),
    removeRouteName: vi.fn(), removeRoutePaths: vi.fn(), setRoutePaths: vi.fn()
  });
  themeStore = reactive({ darkMode: ref(false), setRefreshPage: vi.fn() });
  push.mockReset().mockImplementation(async (path: string) => { routeStore.currentRoute = routeStore.tabsList.find((t: any) => t.path === path); });
});

describe("tab context actions", () => {
  it.each([
    ["close-current", ["/home", "/a", "/c"], undefined],
    ["close-left-side", ["/home", "/b", "/c"], undefined],
    ["close-right-side", ["/home", "/a", "/b"], "/b"],
    ["close-other", ["/home", "/b"], "/b"],
    ["close-all", ["/home"], "/home"]
  ])("%s uses the selected tab and preserves pinned tabs", async (action, remaining, destination) => {
    const wrapper = renderTabs();
    const menu = wrapper.findAll(".dropdown")[2];
    expect(menu, "each tab needs its own context menu").toBeDefined();
    await menu.findAll("button").find(button => button.text() === `system.${action}`)!.trigger("click");
    await flushPromises();
    expect(routeStore.tabsList.map((t: any) => t.path)).toEqual(remaining);
    const removed = ["/home", "/a", "/b", "/c"].filter(path => !(remaining as string[]).includes(path));
    expect(routeStore.removeRoutePaths).toHaveBeenCalledWith(removed);
    if (destination) expect(push).toHaveBeenCalledWith(destination);
    else expect(push).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("refreshes the selected tab instead of the previously active page", async () => {
    const wrapper = renderTabs();
    const menu = wrapper.findAll(".dropdown")[2];
    expect(menu).toBeDefined();
    await menu.findAll("button").find(button => button.text() === "system.refresh")!.trigger("click");
    await flushPromises();
    expect(push).toHaveBeenCalledWith("/b");
    expect(routeStore.removeRouteName).toHaveBeenCalledWith("/b");
    expect(routeStore.setRoutePaths).toHaveBeenCalledWith("/b");
    expect(themeStore.setRefreshPage.mock.calls).toEqual([[false], [true]]);
    wrapper.unmount();
  });

  it("disables closing a pinned tab", () => {
    const wrapper = renderTabs();
    const menu = wrapper.findAll(".dropdown")[0];
    const close = menu.findAll("button").find(button => button.text() === "system.close-current")!;
    expect(close.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });
});
