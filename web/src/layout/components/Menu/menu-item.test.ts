import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import MenuItem from "./menu-item.vue";
import { MEDIA_MENU_SECTIONS } from "./media-menu-sections";

function route(path: string, type = 2, hide = false, children?: Menu.MenuOptions[]): Menu.MenuOptions {
  return {
    id: path,
    parentId: "0",
    path,
    name: path,
    meta: { title: path, hide, disable: false, keepAlive: false, affix: false, roles: [], type },
    children
  };
}

const stubs = {
  "a-sub-menu": { template: "<nav class='sub-menu'><div class='sub-title'><slot name='title' /></div><slot /></nav>" },
  "a-menu-item": { template: "<button class='route-item'><slot name='icon' /><slot /></button>" },
  "a-menu-item-group": { template: "<section class='item-group'><h2><slot name='title' /></h2><slot /></section>" },
  MenuItemIcon: { template: "<i class='menu-icon' />" }
};

function mountMenu(children: Menu.MenuOptions[], parentPath = "/media") {
  return mount(MenuItem, {
    props: { routeTree: [route(parentPath, 1, false, children)] },
    global: {
      stubs,
      mocks: { $t: (key: string) => key }
    }
  });
}

describe("MenuItem media grouping", () => {
  it("renders four non-clickable headings while keeping every route as a real menu item", () => {
    const stable = MEDIA_MENU_SECTIONS.flatMap(section => section.paths).toReversed().map(path => route(path));
    const wrapper = mountMenu([...stable, route("/gb28181/zlm/future")]);

    expect(wrapper.findAll(".item-group")).toHaveLength(4);
    expect(wrapper.findAll(".item-group > h2").map(item => item.text())).toEqual([
      "menu.media-section-cluster",
      "menu.media-section-monitoring",
      "menu.media-section-ingress",
      "menu.media-section-capabilities"
    ]);
    expect(wrapper.findAll(".route-item")).toHaveLength(14);
    expect(wrapper.findAll(".route-item").at(-1)?.text()).toContain("menu./gb28181/zlm/future");
    expect(wrapper.findAll(".item-group > h2 button")).toHaveLength(0);
  });

  it("does not render empty headings after RBAC removes a complete group", () => {
    const wrapper = mountMenu([
      route("/gb28181/zlm/overview", 2, true),
      route("/gb28181/zlm/streams")
    ]);

    expect(wrapper.findAll(".item-group")).toHaveLength(1);
    expect(wrapper.text()).toContain("menu.media-section-monitoring");
    expect(wrapper.text()).not.toContain("menu.media-section-cluster");
  });

  it("leaves non-media route trees on the existing recursive renderer", () => {
    const wrapper = mountMenu([route("/system/account")], "/system");

    expect(wrapper.findAll(".item-group")).toHaveLength(0);
    expect(wrapper.findAll(".route-item")).toHaveLength(1);
  });
});
