import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";

import { useRouteConfigStore } from "./route-config";

describe("route config remains the authorization truth", () => {
  it("stores the backend route tree and never injects visual media groups", () => {
    const source = readFileSync(resolve(process.cwd(), "src/store/modules/route-config.ts"), "utf8");

    expect(source).toContain("routeTree.value = data");
    expect(source).toContain("routeList.value = flatRoute");
    expect(source).not.toMatch(/media-menu-sections|menu-item-group|media-section-/i);
  });
});

function workbenchRoute(path: string, name: string, title: string) {
  return {
    id: name,
    parentId: "media",
    path,
    name,
    meta: {
      title,
      hide: false,
      disable: false,
      keepAlive: true,
      affix: false,
      link: "",
      iframe: false,
      isFull: false,
      roles: [],
      permission: "",
      svgIcon: "",
      icon: "",
      sort: 0,
      type: 2
    }
  } as Menu.MenuOptions;
}

describe("route config media workbench tabs", () => {
  beforeEach(() => {
    vi.stubGlobal("ref", ref);
    setActivePinia(createPinia());
  });

  it("opens each workbench menu in its own tab", () => {
    const store = useRouteConfigStore();
    const overview = workbenchRoute("/media/overview", "media-overview", "运行总览");
    const nodes = workbenchRoute("/media/nodes", "media-nodes", "节点管理");
    store.routeList = [overview, nodes];

    store.setTabs(overview);
    expect(store.tabsList).toHaveLength(1);
    expect(store.tabsList[0]).toMatchObject({
      path: "/media/overview",
      meta: { title: "运行总览" }
    });

    store.setTabs(nodes);
    expect(store.tabsList).toHaveLength(2);
    expect(store.tabsList).toMatchObject([
      { path: "/media/overview", meta: { title: "运行总览" } },
      { path: "/media/nodes", meta: { title: "节点管理" } }
    ]);
  });
});
