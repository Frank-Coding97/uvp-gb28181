import { describe, expect, it } from "vitest";

import { convertMenuItemsToRoutes, type MenuItem } from "./menu";

function backendMenu(path: string, title: string, options: Partial<MenuItem> = {}): MenuItem {
  return {
    id: options.id ?? 1,
    createdAt: "2026-08-30T00:00:00Z",
    updatedAt: "2026-08-30T00:00:00Z",
    deletedAt: null,
    parentId: options.parentId ?? 0,
    path,
    name: options.name ?? path,
    component: options.component ?? "",
    permission: options.permission ?? "",
    redirect: options.redirect ?? "",
    title,
    isFull: false,
    hide: options.hide ?? false,
    disable: false,
    keepAlive: false,
    affix: false,
    link: "",
    iframe: false,
    svgIcon: "",
    icon: "",
    sort: options.sort ?? 0,
    type: options.type ?? 2,
    createdBy: 1,
    children: options.children ?? null,
    apis: null
  };
}

describe("media menu route compatibility", () => {
  it("keeps /media as a two-level direct-page tree without recording menus", () => {
    const media = backendMenu("/media", "流媒体管理", {
      type: 1,
      redirect: "/gb28181/zlm/overview",
      children: [
        backendMenu("/gb28181/zlm/overview", "集群总览"),
        backendMenu("/gb28181/zlm/nodes", "节点管理"),
        backendMenu("/gb28181/zlm/runtime", "总览"),
        backendMenu("/gb28181/zlm/streams", "流管理")
      ]
    });

    const [converted] = convertMenuItemsToRoutes([media]);

    expect(converted.path).toBe("/media");
    expect(converted.redirect).toBe("/gb28181/zlm/overview");
    expect(converted.children?.map(item => item.path)).toEqual([
      "/gb28181/zlm/overview",
      "/gb28181/zlm/nodes",
      "/gb28181/zlm/runtime",
      "/gb28181/zlm/streams"
    ]);
    expect(converted.children?.map(item => item.path)).not.toContain("/gb28181/cloud-recordings");
    expect(converted.children?.every(item => item.meta.type === 2)).toBe(true);
  });

  it("preserves the hidden legacy node detail route without making it a group level", () => {
    const [detail] = convertMenuItemsToRoutes([
      backendMenu("/gb28181/zlm/nodes/:id", "节点详情", { hide: true, component: "gb28181/zlm/NodeDetail" })
    ]);

    expect(detail).toMatchObject({
      path: "/gb28181/zlm/nodes/:id",
      component: "gb28181/zlm/NodeDetail",
      meta: { hide: true, type: 2 }
    });
  });

  it("marks only the compatibility component so router history can skip transient tabs", () => {
    const [legacy, regular] = convertMenuItemsToRoutes([
      backendMenu("/gb28181/zlm/streams", "流媒体", {
        hide: true,
        component: "gb28181/zlm/workbench/LegacyMediaRoute"
      }),
      backendMenu("/media/monitoring", "媒体监控", {
        component: "gb28181/zlm/workbench/MediaMonitoring"
      })
    ]);

    expect(legacy.meta.legacyMedia).toBe(true);
    expect(regular.meta.legacyMedia).toBe(false);
  });
});
