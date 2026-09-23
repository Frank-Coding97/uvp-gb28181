import { mount } from "@vue/test-utils";
import { reactive, ref, nextTick } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Header from "./index.vue";

const isMobile = ref(false);
const theme = reactive({ isTabs: ref(true) });
vi.mock("@/hooks/useDevicesSize", () => ({ useDevicesSize: () => ({ isMobile }) }));
vi.mock("@/store/modules/theme-config", () => ({ useThemeConfig: () => theme }));
vi.mock("@/layout/components/Header/components/header-left/index.vue", () => ({ default: { template: "<button>菜单</button>" } }));
vi.mock("@/layout/components/Header/components/header-right/index.vue", () => ({ default: { template: "<button>账号</button>" } }));
vi.mock("@/layout/components/Tabs/index.vue", () => ({ default: { template: "<nav>标签栏</nav>" } }));

beforeEach(() => { isMobile.value = false; theme.isTabs = true; });

describe("mobile header", () => {
  it("hides tabs on mobile and restores them without changing the user's preference", async () => {
    const wrapper = mount(Header, { global: { stubs: { "a-layout-header": { template: "<header><slot /></header>" } } } });
    expect(wrapper.find("nav").exists()).toBe(true);
    isMobile.value = true;
    await nextTick();
    expect(wrapper.find("nav").exists()).toBe(false);
    expect(wrapper.classes()).toContain("header--compact");
    expect(wrapper.findAll("button").map(button => button.text())).toEqual(["菜单", "账号"]);
    expect(theme.isTabs).toBe(true);
    isMobile.value = false;
    await nextTick();
    expect(wrapper.find("nav").exists()).toBe(true);
    expect(wrapper.classes()).not.toContain("header--compact");
    wrapper.unmount();
  });

  it("keeps the account at the right when tabs are disabled on desktop", () => {
    theme.isTabs = false;
    const wrapper = mount(Header, { global: { stubs: { "a-layout-header": { template: "<header><slot /></header>" } } } });
    expect(wrapper.find("nav").exists()).toBe(false);
    expect(wrapper.classes()).toContain("header--compact");
    wrapper.unmount();
  });
});
