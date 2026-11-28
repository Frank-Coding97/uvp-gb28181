/**
 * 「侧边栏点『前往升级』→ 升级页落地自动检查」的端到端用例：用**真路由器**，
 * 并照搬布局层（layout/components/Main/index.vue）的做法 —— 页面组件的 key 就是整条地址。
 *
 * ⛔ 为什么单靠 index.test.ts 不够：那边路由器是空桩，看不出「地址一变、页面就被重建」。
 *    2026-11 曾经的实现是「带 ?check=1 跳转 → 升级页查完把参数抹掉」，在桩路由器下用例全绿，
 *    真实环境里抹参数那一下把页面重建了，查到的结果跟着旧实例被丢掉 ——
 *    用户看到的就是「进来还得再手点一次检查」。
 */
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  getStatus: vi.fn(),
  getMigrationPlan: vi.fn(),
  check: vi.fn(),
  getNotice: vi.fn()
}));

vi.mock("@arco-design/web-vue", () => ({ Message: { success: vi.fn(), error: vi.fn() }, Modal: { confirm: vi.fn() } }));
vi.mock("@/hooks/usePermission", () => ({ hasPermission: () => true }));
vi.mock("@/api/service-upgrade", () => ({
  getServiceUpgradeStatus: api.getStatus,
  getServiceUpgradeMigrationPlan: api.getMigrationPlan,
  checkServiceUpgrade: api.check,
  getServiceUpgradeNotice: api.getNotice,
  importServiceUpgrade: vi.fn(),
  getServiceUpgradeImport: vi.fn(),
  createServiceUpgradeTask: vi.fn(),
  applyServiceUpgradeTask: vi.fn(),
  cancelServiceUpgradeTask: vi.fn(),
  rollbackServiceUpgradeTask: vi.fn(),
  ignoreServiceUpgradeNotice: vi.fn(),
  clearIgnoredServiceUpgradeNotice: vi.fn(),
  setServiceUpgradeAutoCheck: vi.fn()
}));

import { APP_VERSION } from "@/config/version";
import { consumeUpgradeAutoCheck, requestUpgradeAutoCheck } from "@/hooks/useUpgradeNotice";
import ServiceUpgradePage from "./index.vue";

// 照布局层：这个页面的 key 就是整条地址（resolveMediaRouteRenderKey 的兜底分支）。
const Host = defineComponent({
  setup() {
    return () =>
      h(RouterView, null, {
        default: ({ Component, route }: any) => h(Component, { key: route.fullPath })
      });
  }
});

const stubs = {
  "a-drawer": { props: ["visible"], template: "<aside v-if='visible'><slot name='title' /><slot /></aside>" },
  "a-card": { template: "<section><slot name='title' /><slot name='extra' /><slot /></section>" },
  "a-button": { props: { disabled: Boolean }, template: "<button :disabled='disabled'><slot /></button>" },
  "a-descriptions": { template: "<div><slot /></div>" },
  "a-descriptions-item": { template: "<div><slot /></div>" },
  "a-space": { template: "<span><slot /></span>" },
  "a-upload": { template: "<div><slot name='upload-button' /></div>" },
  "a-switch": { template: "<input type='checkbox' />" },
  "a-alert": { props: ["type"], template: "<div role='alert'><slot /><slot name='action' /></div>" },
  "a-table": { template: "<div><slot name='columns' /></div>" },
  "a-table-column": { template: "<div />" },
  "a-link": { props: ["disabled"], template: "<a :aria-disabled='disabled'><slot name='icon' /><slot /></a>" },
  "a-tag": { template: "<span><slot /></span>" },
  "icon-refresh": true,
  "icon-file": true,
  "icon-eye": true,
  "icon-close-circle": true,
  "icon-arrow-up": true,
  "icon-undo": true
};

const mountAt = async (path: string) => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/system/service-upgrade", component: ServiceUpgradePage }]
  });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(Host, { global: { plugins: [router], stubs } });
  await flushPromises();
  return { wrapper, router };
};

describe("前往升级：真路由器下的落地自动检查", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    consumeUpgradeAutoCheck();
    api.check.mockReset();
    api.getStatus.mockReset().mockResolvedValue({
      code: 0,
      data: {
        current: { version: APP_VERSION, build_id: "current" },
        deployment: "systemd",
        releaseSourceConfigured: true,
        capabilities: { onlineCheck: true, offlineImport: true, apply: true, rollback: true, updaterProtocol: 1 },
        tasks: [],
        events: []
      }
    });
    api.getMigrationPlan.mockReset().mockResolvedValue({
      code: 0,
      data: { can_apply: true, pending: [], applied: [], unknown: [], destructive: [], dialect: "sqlite" }
    });
    api.getNotice.mockReset().mockResolvedValue({
      code: 0,
      data: { hasUpdate: false, ignored: false, checkEnabled: true, autoCheck: true }
    });
    api.check.mockResolvedValue({
      code: 0,
      data: {
        manifest: { version: "1.2.0", build_id: "next", release_notes: "修复若干问题", artifacts: [] },
        artifact: { sha256: "a".repeat(64) },
        hasUpdate: true
      }
    });
  });

  it("落地查一次，查到的版本留在页面上，地址保持干净", async () => {
    // 照 Logo 的 goUpgrade：先登记信号，再跳到干净的地址。
    requestUpgradeAutoCheck();
    const { wrapper, router } = await mountAt("/system/service-upgrade");

    expect(api.check).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.fullPath).toBe("/system/service-upgrade");
    expect(wrapper.text()).toContain("待升级版本 v1.2.0");
    wrapper.unmount();
  });

  // 同页跳转不重建组件：必须有监听，否则点了「前往升级」什么都不会发生。
  it("页面已经开着时再点「前往升级」也会查一次", async () => {
    const { wrapper, router } = await mountAt("/system/service-upgrade");
    expect(api.check).not.toHaveBeenCalled();

    requestUpgradeAutoCheck();
    await router.push("/system/service-upgrade");
    await flushPromises();

    expect(api.check).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("待升级版本 v1.2.0");
    wrapper.unmount();
  });
});
