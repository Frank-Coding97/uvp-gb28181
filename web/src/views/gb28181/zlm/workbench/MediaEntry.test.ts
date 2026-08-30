import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const router = vi.hoisted(() => ({ replace: vi.fn() }));
const authorizedRoutes = ref<Menu.MenuOptions[]>([]);

vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => router
}));
vi.mock("@/store/modules/route-config", async () => {
  const { defineStore } = await import("pinia");
  return {
    useRouteConfigStore: defineStore("route-config", () => ({ routeList: authorizedRoutes }))
  };
});

import { useRouteConfigStore } from "@/store/modules/route-config";
import MediaEntry from "./MediaEntry.vue";

function authorizedRoute(path: string) {
  return { path, name: path, meta: { title: path, hide: true } } as Menu.MenuOptions;
}

describe("MediaEntry", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    router.replace.mockReset();
  });

  async function mountWith(paths: string[]) {
    authorizedRoutes.value = paths.map(authorizedRoute);
    useRouteConfigStore();
    const wrapper = mount(MediaEntry);
    await flushPromises();
    return wrapper;
  }

  it("replaces the root with overview for a fully authorized administrator", async () => {
    const wrapper = await mountWith([
      "/gb28181/zlm/overview",
      "/gb28181/zlm/streams",
      "/gb28181/zlm/proxies",
      "/gb28181/cloud-recordings",
      "/gb28181/zlm/nodes",
      "/gb28181/zlm/scheduler"
    ]);

    expect(router.replace).toHaveBeenCalledOnce();
    expect(router.replace).toHaveBeenCalledWith("/media/overview");
    wrapper.unmount();
  });

  it("chooses the first authorized workspace instead of assuming overview", async () => {
    const wrapper = await mountWith(["/gb28181/zlm/rtp-servers"]);

    expect(router.replace).toHaveBeenCalledOnce();
    expect(router.replace).toHaveBeenCalledWith("/media/ingress");
    wrapper.unmount();
  });

  it("shows an explicit no-permission state without making a request", async () => {
    const wrapper = await mountWith([]);

    expect(router.replace).not.toHaveBeenCalled();
    expect(wrapper.get("[role='alert']").text()).toContain("没有流媒体管理权限");
    expect(wrapper.text()).toContain("联系管理员");
    wrapper.unmount();
  });
});
